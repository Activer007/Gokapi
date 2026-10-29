//go:build test

package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
)

type sessionDeleteBarrier struct {
	dbabstraction.Database
	deleted, resume chan struct{}
}

func (b sessionDeleteBarrier) DeleteSession(id string) {
	b.Database.DeleteSession(id)
	if id == "synthetic-old" {
		close(b.deleted)
		<-b.resume
	}
}

// A revoke-all operation must also remove a replacement being saved by a
// rotation that won the lock first. Both stores use the same facade contract.
func TestSEC021SessionMutationOrdering(t *testing.T) {
	previous := db
	t.Cleanup(func() { db = previous })
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "sessions.sqlite")}
			var server *miniredis.Miniredis
			if backend == "redis" {
				server = miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: server.Addr(), RedisPrefix: "ordering_"}
			}
			store, err := dbabstraction.GetNew(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(store.Close)
			for _, mode := range []string{"user", "all"} {
				t.Run(mode, func(t *testing.T) {
					store.DeleteAllSessions()

					barrier := sessionDeleteBarrier{Database: store, deleted: make(chan struct{}), resume: make(chan struct{})}
					db = barrier
					session := models.Session{UserId: 9021, RenewAt: time.Now().Add(-time.Minute).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()}
					SaveSession("synthetic-old", session)
					other := session
					other.UserId++
					SaveSession("synthetic-other-owner", other)
					rotated := make(chan bool, 1)
					go func() { rotated <- RotateSession("synthetic-old", "synthetic-new", session, session) }()
					select {
					case <-barrier.deleted:
					case result := <-rotated:
						t.Fatalf("rotation ended before barrier: %v", result)
					case <-time.After(5 * time.Second):
						t.Fatal("rotation did not reach barrier")
					}
					revoked := make(chan struct{})
					go func() {
						if mode == "user" {
							DeleteAllSessionsByUser(session.UserId)
						} else {
							DeleteAllSessions()
						}
						close(revoked)
					}()
					select {
					case <-revoked:
						t.Error("revocation completed inside an unfinished rotation")
					case <-time.After(250 * time.Millisecond):
					}
					close(barrier.resume)
					if !<-rotated {
						t.Error("live rotation failed")
					}
					<-revoked
					for _, id := range []string{"synthetic-old", "synthetic-new"} {
						if _, exists := GetSession(id); exists {
							t.Error("revocation left a session usable")
						}
					}
					if _, exists := GetSession("synthetic-other-owner"); exists != (mode == "user") {
						t.Error("revocation affected the wrong owner scope")
					}
				})
			}
		})
	}
}
