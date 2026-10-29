//go:build test && security_whole && security_owner && !integration

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/chunking/chunksession"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// Distinct source IDs avoid a per-file lock serializing all transfers. These
// real management calls compete for the current destination owner's capacity;
// their shared physical blob, instance usage and original request usage stay put.
// Only existing production symbols are used so the old SaveMetaData path is RED
// for accepting excess transfers, rather than failing to compile.
func TestSEC030OwnerQuota(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, item := range []struct {
				dimension string
				count     int
			}{{"bytes", 10}, {"files", 50}} {
				candidate := sec030ChunkCase{backend, "owner", "user", item.dimension, item.count}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030OwnerQuota", candidate) })
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	if candidate.kind != "owner" || candidate.scope != "user" ||
		(candidate.dimension != "bytes" && candidate.dimension != "files") ||
		(candidate.dimension == "bytes" && candidate.count != 10) ||
		(candidate.dimension == "files" && candidate.count != 50) {
		t.Fatal("CHUNK_QUOTA_OWNER_SETUP invalid isolated case")
	}
	sec030WholeSetup(t, candidate.backend)
	const sourceOwner, actorID = sec030WholeOwner + 1, sec030WholeOwner + 2
	database.SaveUser(models.User{Id: sourceOwner, Name: "synthetic-transfer-source", UserLevel: models.UserLevelUser}, false)
	actor := models.User{Id: actorID, Name: "synthetic-transfer-admin", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermEditOtherUploads}
	database.SaveUser(actor, false)
	hasHold := candidate.dimension == "bytes"
	transferCount := candidate.count
	if hasHold {
		transferCount-- // One real first-chunk admission competes with transfers.
	}
	sources := make([]models.File, transferCount)
	for i := range sources {
		source := sec030WholeSeed(t, []byte{0x73})
		source.UserId, source.Name, source.ContentType = sourceOwner, "synthetic-transfer.png", "image/png"
		source.Size, source.UploadDate, source.DownloadCount = "1 B", 1234567890, 7
		// Nonzero encryption data must survive exactly; this management path
		// neither reads nor decrypts the shared synthetic canonical bytes.
		source.Encryption = models.EncryptionInfo{IsEncrypted: true, DecryptionKey: []byte{0x6b}, Nonce: []byte{0x6e}}
		storage.AddHotlink(&source)
		if i == 0 {
			// An expired public upload capability is not a management gate.
			parent := models.FileRequest{Id: rand.Text(), UserId: sourceOwner, ApiKey: rand.Text(), MaxFiles: 1, MaxSize: 1, Expiry: time.Now().Add(-time.Hour).Unix()}
			database.SaveFileRequest(parent)
			source.UploadRequestId = parent.Id
		}
		database.SaveMetaData(source)
		if source.HotlinkId == "" {
			t.Fatal("CHUNK_QUOTA_OWNER_SETUP source has no hotlink")
		}
		sources[i] = source
	}
	filler := []byte{0x66}
	if hasHold {
		filler = bytes.Repeat(filler, (1<<20)-2)
	}
	sec030WholeSeed(t, filler)
	var hold uploadquota.Reservation
	if hasHold {
		now := time.Now().Unix()
		var err error
		hold, err = database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Reserve,
			Upload: uploadquota.Reservation{ID: "owner-hold-" + rand.Text(), UserID: sec030WholeOwner, BytesKnown: true, Bytes: 1, StagingBytes: (64 << 10) + 4, ExpiresAt: now + 3600},
			Limits: testRuntimeChunkLimits(t), Now: now})
		if err != nil {
			t.Fatal("CHUNK_QUOTA_OWNER_SETUP active reservation failed")
		}
		defer func() {
			if _, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Abort, Upload: hold, Terminal: uploadquota.Aborted, CleanupConfirmed: true, Now: time.Now().Unix()}); err != nil {
				t.Error("CHUNK_QUOTA_OWNER_CLEANUP synthetic hold could not be released")
			}
		}()
	}
	initial := sec030FinishSnapshot(t)
	usage, err := uploadquota.Measure(initial.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_OWNER_SETUP invalid initial metadata")
	}
	used := usage.Users[sec030WholeOwner]
	if hasHold && used.Bytes+hold.Bytes != (1<<20)-1 || !hasHold && used.Files != 1 {
		t.Fatal("CHUNK_QUOTA_OWNER_SETUP destination must have exactly one byte/file free")
	}
	keys := make([]models.ApiKey, transferCount)
	for i := range keys {
		keys[i] = models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: actorID, Permissions: models.ApiPermEdit}
		database.SaveApiKey(keys[i])
		stored, found := database.GetApiKey(keys[i].Id)
		if !found || stored.UserId != actorID || !stored.HasPermission(models.ApiPermEdit) || !actor.IsAdmin() || !actor.HasPermissionEditOtherUploads() {
			t.Fatal("CHUNK_QUOTA_OWNER_SETUP transfer credentials must be authorized")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(Process))
	defer server.Close()
	client := server.Client()
	client.Timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	uploadScope := chunksession.Scope{ClientID: "owner-vs-upload-" + rand.Text(), UserID: sec030WholeOwner}
	var uploadCall sec030ChunkCall
	if hasHold {
		key := sec030WholeKey(t)
		uploadCall = sec030ChunkAddCall(t, "ordinary", "", key.Id, uploadScope.ClientID, 1, 0, []byte{0x75})
		defer func() {
			if err := storage.ChunkSessions().Cancel(context.Background(), uploadScope); err != nil {
				t.Error("CHUNK_QUOTA_OWNER_CLEANUP competing upload could not be released")
			}
		}()
	}
	// Synchronize the clients before real HTTP calls without substituting a
	// server-side policy snapshot or an artificial receiver rejection.
	start, ready := make(chan struct{}), make(chan struct{}, candidate.count)
	responses := make([]sec030OwnerResponse, candidate.count)
	var workers sync.WaitGroup
	for i := range responses {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			ready <- struct{}{}
			<-start
			if i == transferCount {
				response := sec030ChunkSend(ctx, client, server.URL, uploadCall)
				responses[i] = sec030OwnerResponse{status: response.status, failed: response.failed, ErrorCode: response.ErrorCode, Result: response.Result, ErrorMessage: response.ErrorMessage}
				return
			}
			responses[i] = sec030OwnerSend(ctx, client, server.URL, keys[i].Id, sources[i].Id)
		}(i)
	}
	for range responses {
		<-ready
	}
	close(start)
	workers.Wait()
	accepted := make(map[string]bool)
	quotaDenied, uploadCount := 0, 0
	for i, response := range responses {
		if response.failed {
			t.Error("CHUNK_QUOTA_OWNER_TRANSPORT bounded request or JSON response failed")
			continue
		}
		if response.status == http.StatusOK && ((i == transferCount && response.Result == "OK") || (i < transferCount && response.Id == sources[i].Id)) {
			if i == transferCount {
				uploadCount++
			} else {
				accepted[response.Id] = true
			}
		} else if response.status == http.StatusRequestEntityTooLarge && response.ErrorCode == sec030WholeQuotaCode && response.Result == "error" && response.ErrorMessage != "" {
			quotaDenied++
		} else {
			t.Errorf("CHUNK_QUOTA_OWNER_RESPONSE unexpected status=%d code=%d; want transfer success or 413/21", response.status, response.ErrorCode)
		}
	}
	if len(accepted)+uploadCount != 1 || quotaDenied != candidate.count-1 {
		t.Errorf("CHUNK_QUOTA_OWNER_CAPACITY transfers=%d uploads=%d quota_denied=%d; want total_winners=1 denied=%d", len(accepted), uploadCount, quotaDenied, candidate.count-1)
	}
	after := sec030FinishSnapshot(t)
	if len(after.state.metadata) != len(initial.state.metadata) || !reflect.DeepEqual(initial.hotlinks, after.hotlinks) {
		t.Error("CHUNK_QUOTA_OWNER_EFFECTS transfer changed metadata count or hotlink mappings")
	}
	for id, old := range initial.state.metadata {
		if accepted[id] {
			old.UserId = sec030WholeOwner
		}
		if !reflect.DeepEqual(old, after.state.metadata[id]) {
			t.Error("CHUNK_QUOTA_OWNER_METADATA transfer changed fields beyond the accepted source owner")
		}
	}
	for path, data := range initial.state.payloads {
		if !bytes.Equal(data, after.state.payloads[path]) {
			t.Error("CHUNK_QUOTA_OWNER_BLOB shared canonical bytes changed")
		}
	}
	if uploadCount == 0 && !reflect.DeepEqual(initial.state.payloads, after.state.payloads) {
		t.Error("CHUNK_QUOTA_OWNER_EFFECTS refused requests left staged payloads")
	}
	if len(after.state.ledger.Records) != len(initial.state.ledger.Records)+uploadCount || after.state.ledger.Version != initial.state.ledger.Version {
		t.Error("CHUNK_QUOTA_OWNER_LEDGER transfer created a receipt or refused upload retained a reservation")
	}
	for id, record := range initial.state.ledger.Records {
		if !reflect.DeepEqual(record, after.state.ledger.Records[id]) {
			t.Error("CHUNK_QUOTA_OWNER_LEDGER transfer consumed or changed an existing hold")
		}
	}
	if hasHold {
		record, found := after.state.ledger.Records[chunksession.ID(uploadScope)]
		if found != (uploadCount == 1) || (found && (!record.BytesKnown || record.Bytes != 1 || record.UserID != sec030WholeOwner || record.RequestID != "" || record.State != uploadquota.Uploading || record.Completing || record.StagingReleased || !sec030ChunkHasPayload(after.state.payloads, []byte{0x75}))) {
			t.Error("CHUNK_QUOTA_OWNER_MIXED upload winner lacks its exact scoped declaration and payload")
		}
	}
	measured, err := uploadquota.Measure(after.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_OWNER_USAGE invalid post-transfer metadata")
	}
	want := usage.Users[sec030WholeOwner]
	want.Bytes += int64(len(accepted))
	want.Files += int64(len(accepted))
	sourceWant := usage.Users[sourceOwner]
	sourceWant.Bytes -= int64(len(accepted))
	sourceWant.Files -= int64(len(accepted))
	if measured.Users[sec030WholeOwner] != want || measured.Users[sourceOwner] != sourceWant || !reflect.DeepEqual(usage.Requests, measured.Requests) || sec030OwnerInstance(usage) != sec030OwnerInstance(measured) {
		t.Error("CHUNK_QUOTA_OWNER_USAGE transfer must move only owner charges, retaining instance and original request totals")
	}
	// Retry a source that did not transfer. Full destination capacity must
	// reject without any metadata, hotlink, ledger or physical byte effects.
	for i, source := range sources {
		if accepted[source.Id] {
			continue
		}
		refused := sec030OwnerSend(ctx, client, server.URL, keys[i].Id, source.Id)
		if refused.failed || refused.status != http.StatusRequestEntityTooLarge || refused.ErrorCode != sec030WholeQuotaCode || !reflect.DeepEqual(after, sec030FinishSnapshot(t)) {
			t.Error("CHUNK_QUOTA_OWNER_REFUSAL full-capacity retry did not refuse as 413/21 without effects")
		}
		break
	}
}

func sec030OwnerInstance(snapshot uploadquota.Snapshot) uploadquota.Usage {
	var result uploadquota.Usage
	for _, usage := range snapshot.Users {
		result.Bytes += usage.Bytes
		result.Files += usage.Files
	}
	return result
}

type sec030OwnerResponse struct {
	models.FileApiOutput
	Result       string
	ErrorCode    int
	ErrorMessage string
	status       int
	failed       bool
}

func sec030OwnerSend(ctx context.Context, client *http.Client, baseURL, key, id string) sec030OwnerResponse {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/files/changeOwner", nil)
	if err != nil {
		return sec030OwnerResponse{failed: true}
	}
	request.Header.Set("apikey", key)
	request.Header.Set("id", id)
	request.Header.Set("newOwner", strconv.Itoa(sec030WholeOwner))
	response, err := client.Do(request)
	if err != nil {
		return sec030OwnerResponse{failed: true}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	result := sec030OwnerResponse{status: response.StatusCode}
	if err != nil || len(data) > 16<<10 || json.Unmarshal(data, &result) != nil {
		result.failed = true
	}
	return result
}
