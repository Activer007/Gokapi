//go:build test

package securityfixture_test

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
)

// Exercise the actual login and persistence path, not a hand-written rehash.
// The legacy fixture uses v2.2.4's SHA1(password + global salt) representation.
func TestSEC012LegacyLoginMigration(t *testing.T) {
	t.Chdir(t.TempDir())
	testconfiguration.Create(false)
	configuration.Load()
	const password, salt = "synthetic-legacy-password", "synthetic-legacy-salt"
	configuration.Get().Authentication.SaltAdmin = salt
	sum := sha1.Sum([]byte(password + salt))
	originalHash := hex.EncodeToString(sum[:])
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{Type: 0, HostUrl: filepath.Join(t.TempDir(), "legacy.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec012_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			user := models.User{Id: 9012, Name: "synthetic-legacy", Password: originalHash, UserLevel: models.UserLevelUser}
			database.SaveUser(user, false)
			login := func(candidate string) bool {
				_, ok, csrfOK := authentication.IsCorrectUsernameAndPassword(user.Name, candidate, csrftoken.Generate(csrftoken.TypeLogin))
				if !csrfOK {
					t.Fatal("fixture CSRF rejected")
				}
				return ok
			}
			if login("wrong-password") {
				t.Fatal("wrong legacy password accepted")
			}
			got, _ := database.GetUser(user.Id)
			if got.Password != originalHash {
				t.Fatal("failed login changed legacy hash")
			}
			if !login(password) {
				t.Fatal("valid legacy login rejected")
			}
			got, _ = database.GetUser(user.Id)
			if got.Password == originalHash || !strings.HasPrefix(got.Password, "argon2id$") {
				t.Fatal("login did not upgrade hash")
			}
			upgraded := got.Password
			database.Close()
			database.Connect(cfg)
			got, _ = database.GetUser(user.Id)
			if got.Password != upgraded {
				t.Fatal("upgrade did not survive database reopen")
			}
			if !login(password) || login("wrong-password") {
				t.Fatal("upgraded login verification is incorrect")
			}
			got, _ = database.GetUser(user.Id)
			if got.Password != upgraded {
				t.Fatal("Argon2id login unexpectedly rewrote the hash")
			}
			// A downgrade cannot consume the upgraded representation. Restore
			// the pre-upgrade record (representing the isolated backup) first.
			if upgraded == originalHash {
				t.Fatal("legacy representation unexpectedly unchanged")
			}
			database.SaveUser(user, false)
			database.Close()
			database.Connect(cfg)
			restored, _ := database.GetUser(user.Id)
			if restored.Password != originalHash {
				t.Fatal("backup record did not restore")
			}
			valid, legacy := configuration.VerifyPassword(password, restored.Password, salt)
			if !valid || !legacy {
				t.Fatal("restored legacy backup cannot authenticate")
			}
		})
	}
}
