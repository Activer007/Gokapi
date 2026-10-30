package webserver

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/webserver/api"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
)

const collectionRetryWindow = 2 * time.Minute
const collectionRetryCapacity = 256

// A consumed presign remains single-use. Collection retries additionally require
// an HttpOnly secret and the same currently authorized logged-in consumer.
type collectionRetry struct {
	mu       sync.Mutex
	grant    models.Presign
	actorID  int
	secret   string
	deadline time.Time
	download *storage.ManagementDownload
	charged  bool
	active   int
}

var collectionRetries = struct {
	sync.Mutex
	grants map[string]*collectionRetry
}{grants: make(map[string]*collectionRetry)}

func collectionRetryCookie(id string) string {
	return fmt.Sprintf("ss_collection_%x", sha256.Sum256([]byte(id)))
}

func serveCollectionPresign(w http.ResponseWriter, r *http.Request, grant models.Presign, files []models.File, actor models.User) {
	if !storage.ValidateManagementRange(r, files, grant.IsArchive()) {
		http.Error(w, "Invalid download range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodHead && !presign.Consume(grant.Id) {
		responseError(w, storage.ErrorInvalidPresign)
		return
	}
	download, err := storage.PrepareManagementDownload(r.Context(), files, actor.Id, storage.ManagementDownloadOptions{
		Count: r.Method != http.MethodHead, Archive: grant.IsArchive(),
	})
	if err != nil {
		responseError(w, storage.ErrorFileNotFound)
		return
	}
	if r.Method == http.MethodHead {
		defer download.Close()
		download.Serve(w, r, grant.Filename)
		return
	}
	lease := &collectionRetry{grant: grant, actorID: actor.Id, secret: rand.Text(),
		deadline: time.Now().Add(collectionRetryWindow), download: download, charged: download.Charged, active: 1}
	collectionRetries.Lock()
	if len(collectionRetries.grants) >= collectionRetryCapacity {
		// Capacity pressure disables retries for this transfer, never admission
		// or the allowance limit. No unbounded capability map is allocated.
		collectionRetries.Unlock()
		defer download.Close()
		download.Serve(w, r, grant.Filename)
		return
	}
	collectionRetries.grants[grant.Id] = lease
	collectionRetries.Unlock()
	base, _ := url.Parse(configuration.Get().ServerUrl)
	cookiePath := "/downloadPresigned"
	if base != nil {
		cookiePath = strings.TrimRight(base.Path, "/") + cookiePath
	}
	http.SetCookie(w, &http.Cookie{Name: collectionRetryCookie(grant.Id), Value: lease.secret,
		Path: cookiePath, MaxAge: int(collectionRetryWindow.Seconds()), Expires: lease.deadline,
		HttpOnly: true, Secure: configuration.UsesHTTPS(), SameSite: http.SameSiteStrictMode})
	time.AfterFunc(collectionRetryWindow, func() { finishCollectionRetry(grant.Id, lease, false) })
	defer finishCollectionRetry(grant.Id, lease, true)
	download.Serve(w, r, grant.Filename)
}

func finishCollectionRetry(id string, lease *collectionRetry, finished bool) {
	collectionRetries.Lock()
	defer collectionRetries.Unlock()
	if finished {
		lease.active--
	}
	if lease.active == 0 && !time.Now().Before(lease.deadline) {
		if collectionRetries.grants[id] == lease {
			delete(collectionRetries.grants, id)
		}
		lease.download.Close()
	}
}

// tryCollectionRetry returns false only when no matching retry authorization
// exists. A failed authentication never yields bytes or extends the deadline.
func tryCollectionRetry(w http.ResponseWriter, r *http.Request, id string) bool {
	cookie, err := r.Cookie(collectionRetryCookie(id))
	if err != nil {
		return false
	}
	collectionRetries.Lock()
	lease := collectionRetries.grants[id]
	if lease == nil || !time.Now().Before(lease.deadline) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(lease.secret)) != 1 {
		collectionRetries.Unlock()
		return false
	}
	lease.active++
	collectionRetries.Unlock()
	defer finishCollectionRetry(id, lease, true)
	actor, err := authentication.GetUserFromRequest(r)
	if err != nil || actor.Id != lease.actorID {
		responseError(w, storage.ErrorInvalidPresign)
		return true
	}
	actor, found := database.GetUser(actor.Id)
	issuer, valid := api.GetDownloadUser(lease.grant.ApiKeyId)
	if !found || !valid || issuer.Id != lease.grant.UserId {
		responseError(w, storage.ErrorInvalidPresign)
		return true
	}
	for _, file := range lease.download.Files {
		if !authorization.Authorize(actor, authorization.ReadFile, file.UserId) || !authorization.Authorize(issuer, authorization.ReadFile, file.UserId) {
			responseError(w, storage.ErrorInvalidPresign)
			return true
		}
	}
	if !storage.ValidateManagementRange(r, lease.download.Files, lease.grant.IsArchive()) {
		http.Error(w, "Invalid download range", http.StatusRequestedRangeNotSatisfiable)
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	// Serialize admissions, not the actual streams: a role downgrade may need
	// to charge this grant once, while several Range retries arrive together.
	lease.mu.Lock()
	download, err := storage.PrepareManagementDownload(r.Context(), lease.download.Files, actor.Id, storage.ManagementDownloadOptions{
		RetryCharged: lease.charged, Count: r.Method != http.MethodHead, Archive: lease.grant.IsArchive(),
	})
	if err == nil {
		lease.charged = download.Charged
	}
	lease.mu.Unlock()
	if err != nil {
		responseError(w, storage.ErrorFileNotFound)
		return true
	}
	defer download.Close()
	download.Serve(w, r, lease.grant.Filename)
	return true
}
