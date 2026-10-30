//go:build test && security_cleanup && !noaws && !awsmock

package securityfixture_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/alicebob/miniredis/v2"
)

// A failed confirmation cannot undo a successful physical DELETE. The original
// metadata remains the accounting/retry anchor until a later checked probe
// confirms absence, rather than reporting a completed retirement prematurely.
func TestSEC040CleanupPostDeleteProbeUnknown(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			var redisAddress string
			if backend == "redis" {
				redisAddress = miniredis.RunT(t).Addr()
			}
			sec040CleanupOpen(t, t.TempDir())
			if redisAddress != "" {
				database.Close()
				database.Connect(models.DbConnection{Type: 1, HostUrl: redisAddress, RedisPrefix: "postdelete_probe_"})
				database.SaveUser(models.User{Id: 7002, Name: "synthetic-postdelete-owner", UserLevel: models.UserLevelUser}, false)
			}
			remote := securityfixture.NewS3(t)
			payload := []byte("synthetic SEC040 post-delete confirmation")
			file := sec040CleanupFile("postdelete-probe", "security-fixture", payload)
			objectPath := "/" + file.AwsBucket + "/" + file.SHA1
			target, err := url.Parse(remote.Server.URL)
			if err != nil {
				t.Fatal("SEC040_SETUP could not parse the loopback endpoint")
			}
			var deleted, failConfirmation atomic.Bool
			var healthyHeads, uncertainHeads atomic.Int64
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
				w.WriteHeader(http.StatusBadGateway)
			}
			proxy.ModifyResponse = func(response *http.Response) error {
				if response.Request.URL.Path != objectPath {
					return nil
				}
				if response.Request.Method == http.MethodHead && response.StatusCode == http.StatusOK {
					healthyHeads.Add(1)
				}
				if response.Request.Method == http.MethodDelete && response.StatusCode >= 200 && response.StatusCode < 300 {
					deleted.Store(true)
				}
				return nil
			}
			gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead && r.URL.Path == objectPath && deleted.Load() && failConfirmation.Load() {
					uncertainHeads.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(gate.Close)
			sec040CleanupLogin(t, gate.URL)
			sec040CleanupUpload(t, file, payload)
			sec040CleanupSave(file)
			before, err := uploadquota.Measure(database.GetAllMetadata())
			if err != nil || before.Users[file.UserId] != (uploadquota.Usage{Files: 1, Bytes: int64(len(payload))}) {
				t.Fatal("SEC040_SETUP synthetic logical usage was not charged")
			}
			ledger, err := database.ReadUploadQuota(t.Context())
			if err != nil {
				t.Fatal("SEC040_SETUP could not read the reservation ledger")
			}
			initialHeads, initialDeletes := healthyHeads.Load(), remote.DeleteAttempts.Load()
			failConfirmation.Store(true)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			result, cleanupErr := storage.CleanUpContext(ctx)
			cancel()
			if cleanupErr == nil || result.Retired != 0 || result.Deleted != 0 || result.Shared != 0 || result.Missing != 0 {
				t.Fatal("SEC040_UNKNOWN post-delete probe failure reported confirmed retirement")
			}
			if healthyHeads.Load() <= initialHeads || !deleted.Load() || remote.DeleteAttempts.Load() != initialDeletes+1 || uncertainHeads.Load() == 0 {
				t.Fatal("SEC040_FAULT did not reach initial HEAD, successful DELETE, then uncertain confirmation")
			}
			sec040CleanupRetained(t, file)
			stored, found, err := database.ReadRecoveryFile(t.Context(), file.Id)
			if err != nil || !found || !reflect.DeepEqual(stored, file) {
				t.Fatal("SEC040_RETAIN unknown confirmation changed the original retry metadata")
			}
			after, err := uploadquota.Measure(database.GetAllMetadata())
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("SEC040_RETAIN unknown confirmation released logical usage")
			}
			checkLedger := func() {
				t.Helper()
				current, err := database.ReadUploadQuota(t.Context())
				if err != nil || !reflect.DeepEqual(current, ledger) {
					t.Fatal("SEC040_ACCOUNTING cleanup altered the reservation ledger")
				}
			}
			checkLedger()
			// The underlying object is already gone. Only the newly successful
			// confirmation, rather than an assumed rollback, permits retirement.
			failConfirmation.Store(false)
			sec040CleanupObject(t, file, 0, false)
			result, err = storage.CleanUpContext(t.Context())
			if err != nil || result.Retired != 1 || result.Missing != 1 || result.Deleted != 0 || result.Shared != 0 {
				t.Fatal("SEC040_RECOVERY confirmed absence did not retire exactly the missing share")
			}
			sec040CleanupRetired(t, file)
			after, err = uploadquota.Measure(database.GetAllMetadata())
			if err != nil || after.Users[file.UserId] != (uploadquota.Usage{}) {
				t.Fatal("SEC040_RECOVERY confirmed retirement did not release its logical usage")
			}
			checkLedger()
			result, err = storage.CleanUpContext(t.Context())
			if err != nil || result.Retired != 0 || result.Deleted != 0 || result.Shared != 0 || result.Missing != 0 || remote.DeleteAttempts.Load() != initialDeletes+1 {
				t.Fatal("SEC040_IDEMPOTENT repeated cleanup retired or deleted the same share again")
			}
			checkLedger()
		})
	}
}
