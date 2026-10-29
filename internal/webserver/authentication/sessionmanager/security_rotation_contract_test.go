//go:build test

package sessionmanager

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

// Model a request that already read its session before a revocation commits.
// This is a deterministic handler-state contract, not a browser exploit claim.
func TestSEC021SessionRotationSnapshot(t *testing.T) {
	const child = "GOKAPI_SEC021_ROTATION_CHILD"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSEC021SessionRotationSnapshot$", "-test.v")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), child+"=1")
		output, err := cmd.CombinedOutput()
		t.Logf("isolated session contract:\n%s", output)
		if err != nil {
			t.Fatalf("session contract failed: %v", err)
		}
		return
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "session.sqlite")}
			var server *miniredis.Miniredis
			if backend == "redis" {
				server = miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: server.Addr(), RedisPrefix: "rotation_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			user := models.User{Id: 9021, Name: "synthetic-session-owner", UserLevel: models.UserLevelUser}
			database.SaveUser(user, false)
			for _, state := range []string{"live", "expired", "logout", "revoke-user", "revoke-all", "changed-snapshot", "concurrent"} {
				t.Run(state, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						if server != nil {
							server.SetTime(time.Now())
						}
						id := rand.Text()
						snapshot := models.Session{UserId: user.Id, RenewAt: time.Now().Add(-time.Minute).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()}
						if state == "expired" {
							snapshot.ValidUntil = time.Now().Add(-time.Second).Unix()
						}
						database.SaveSession(id, snapshot)
						switch state {
						case "logout":
							r := httptest.NewRequest("GET", "/logout", nil)
							r.AddCookie(&http.Cookie{Name: "session_token", Value: id})
							LogoutSession(httptest.NewRecorder(), r)
						case "revoke-user":
							database.DeleteAllSessionsByUser(user.Id)
						case "revoke-all":
							database.DeleteAllSessions()
						case "changed-snapshot":
							changed := snapshot
							changed.RenewAt = time.Now().Add(time.Hour).Unix()
							database.SaveSession(id, changed)
						}
						count := 1
						if state == "concurrent" {
							count = 16
						}
						start := make(chan struct{})
						var wg sync.WaitGroup
						var accepted, issued atomic.Int32
						for i := 0; i < count; i++ {
							wg.Add(1)
							go func() {
								defer wg.Done()
								<-start
								w := httptest.NewRecorder()
								if useSession(w, id, snapshot, false, 1) {
									accepted.Add(1)
								}
								for _, cookie := range w.Result().Cookies() {
									if cookie.Name == "session_token" && cookie.Value != "" {
										r := httptest.NewRequest("GET", "/admin", nil)
										r.AddCookie(cookie)
										if _, ok := IsValidSession(httptest.NewRecorder(), r, false, 1); ok {
											issued.Add(1)
										}
									}
								}
							}()
						}
						close(start)
						wg.Wait()
						synctest.Wait()
						want := int32(0)
						if state == "live" || state == "concurrent" {
							want = 1
						}
						if accepted.Load() != want || issued.Load() != want {
							t.Fatalf("renewal accepted=%d usable-new-sessions=%d want=%d", accepted.Load(), issued.Load(), want)
						}
					})
				})
			}
		})
	}
}
