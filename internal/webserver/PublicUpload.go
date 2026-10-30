package webserver

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filerequest"
)

// Both URL forms resolve the live collection-only credential and request.
// The short form omits the redundant ID: the key already records that association.
func publicUploadRequest(r *http.Request) (models.FileRequest, bool) {
	token := r.PathValue("token")
	short := token != ""
	if !short {
		token = r.URL.Query().Get("key")
	}
	if len(token) < 5 || len(token) > 128 || strings.IndexFunc(token, func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-')
	}) >= 0 {
		return models.FileRequest{}, false
	}
	key, found := database.GetApiKey(token)
	if !found || !key.IsUploadRequestKey() || (key.Expiry != 0 && key.Expiry <= time.Now().Unix()) {
		return models.FileRequest{}, false
	}
	if !short && r.URL.Query().Get("id") != key.UploadRequestId {
		return models.FileRequest{}, false
	}
	owner, found := database.GetUser(key.UserId)
	if !found || !authorization.CanUseAPI(owner, key, models.ApiPermNone) {
		return models.FileRequest{}, false
	}
	request, found := filerequest.Get(key.UploadRequestId)
	if !found || request.UserId != key.UserId || subtle.ConstantTimeCompare([]byte(request.ApiKey), []byte(token)) != 1 ||
		request.IsExpired() || (!request.IsUnlimitedFiles() && request.UploadedFiles >= request.MaxFiles) {
		return models.FileRequest{}, false
	}
	return request, true
}

// A path-only base preserves the browser's current origin and a reverse proxy's
// configured subdirectory while resolving the existing ./assets and ./api URLs.
func publicUploadBasePath() string {
	base, err := url.Parse(configuration.Get().ServerUrl)
	if err != nil || !strings.HasPrefix(base.Path, "/") {
		return "/"
	}
	return strings.TrimRight(base.Path, "/") + "/"
}
