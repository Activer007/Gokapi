//go:build test

// Package securityfixture supplies disposable actors and resources for security
// regressions. It never loads configuration or connects to a database itself.
package securityfixture

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

type Scene struct {
	DataDir  string
	Users    map[string]models.User
	Keys     map[string]models.ApiKey
	Sessions map[string]models.Session
	Files    map[string]models.File
	Request  models.FileRequest
}

// New creates only synthetic data under t.TempDir. passwordHash must be a hash
// of a synthetic password supplied by the test, never a production credential.
func New(t *testing.T, passwordHash string) Scene {
	t.Helper()
	now := time.Now()
	prefix := rand.Text()
	s := Scene{
		DataDir: t.TempDir(),
		Users: map[string]models.User{
			"admin": {Id: 7001, Name: prefix + "-admin", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll},
			"owner": {Id: 7002, Name: prefix + "-owner", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads},
			"other": {Id: 7003, Name: prefix + "-other", UserLevel: models.UserLevelUser},
		},
		Keys:     make(map[string]models.ApiKey),
		Sessions: make(map[string]models.Session),
		Files:    make(map[string]models.File),
	}
	for name, user := range s.Users {
		s.Keys[name] = models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: user.Id,
			Permissions: models.ApiPermDefault | models.ApiPermDownload | models.ApiPermReplace}
		s.Sessions[rand.Text()] = models.Session{UserId: user.Id, RenewAt: now.Add(time.Hour).Unix(), ValidUntil: now.Add(2 * time.Hour).Unix()}
	}
	for name, expiry := range map[string]int64{"temporary": now.Add(time.Minute).Unix(), "expired": now.Add(-time.Minute).Unix()} {
		key := s.Keys["owner"]
		key.Id, key.PublicId, key.IsSystemKey, key.Expiry = rand.Text(), rand.Text(), true, expiry
		s.Keys[name] = key
	}
	s.Request = models.FileRequest{Id: rand.Text(), UserId: s.Users["owner"].Id, Name: "Synthetic request", MaxFiles: 2, MaxSize: 1, Expiry: now.Add(time.Hour).Unix()}
	requestKey := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: s.Request.UserId, Permissions: models.ApiPermUpload, UploadRequestId: s.Request.Id}
	s.Keys["request"] = requestKey
	s.Request.ApiKey = requestKey.Id
	for _, name := range []string{"own", "other", "dedup", "protected", "expired", "request"} {
		content := []byte("synthetic file " + name)
		owner := s.Users["owner"].Id
		if name == "dedup" {
			content = []byte("synthetic file own")
		}
		if name == "other" || name == "dedup" {
			owner = s.Users["other"].Id
		}
		hash := sha1.Sum(content) // Matches the application's blob naming, not password hashing.
		blob := hex.EncodeToString(hash[:])
		if err := os.WriteFile(filepath.Join(s.DataDir, blob), content, 0600); err != nil {
			t.Fatal(err)
		}
		file := models.File{Id: rand.Text(), Name: name + ".txt", SHA1: blob, SizeBytes: int64(len(content)), ContentType: "text/plain", UserId: owner, UnlimitedTime: true, UnlimitedDownloads: true}
		if name == "protected" {
			file.PasswordHash = passwordHash
		}
		if name == "expired" {
			file.UnlimitedTime = false
			file.ExpireAt = now.Add(-time.Minute).Unix()
		}
		if name == "request" {
			file.UploadRequestId = s.Request.Id
		}
		s.Files[name] = file
	}
	return s
}

// Store is satisfied by both database providers. The caller must supply a new
// isolated database; fixed user IDs intentionally make matrix assertions simple.
type Store interface {
	SaveUser(models.User, bool)
	SaveApiKey(models.ApiKey)
	SaveSession(string, models.Session)
	SaveMetaData(models.File)
	SaveFileRequest(models.FileRequest)
}

func (s Scene) Seed(db Store) {
	for _, user := range s.Users {
		db.SaveUser(user, false)
	}
	for _, key := range s.Keys {
		db.SaveApiKey(key)
	}
	for id, session := range s.Sessions {
		db.SaveSession(id, session)
	}
	for _, file := range s.Files {
		db.SaveMetaData(file)
	}
	db.SaveFileRequest(s.Request)
}
