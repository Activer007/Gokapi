//go:build test && security_whole && security_duplicate && !integration

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
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/chunking/chunksession"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// All fixtures are synthetic. Each provider/capacity combination gets a fresh
// child and real API requests; no new production symbol is needed for RED.
func TestSEC030DuplicateQuota(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, item := range []struct {
				scope, dimension string
				count            int
			}{
				{"user", "bytes", 10}, {"user", "files", 50},
				{"instance", "bytes", 50}, {"instance", "files", 10},
				{"request", "bytes", 10}, {"request", "files", 50},
			} {
				candidate := sec030ChunkCase{backend, "duplicate", item.scope, item.dimension, item.count}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030DuplicateQuota", candidate) })
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	if candidate.kind != "duplicate" || (candidate.dimension != "bytes" && candidate.dimension != "files") {
		t.Fatal("CHUNK_QUOTA_DUP_SETUP invalid isolated case")
	}
	sec030WholeSetup(t, candidate.backend)
	const otherOwner = sec030WholeOwner + 1
	database.SaveUser(models.User{Id: otherOwner, Name: "synthetic-source-owner", UserLevel: models.UserLevelUser}, false)
	crossOwner := candidate.scope == "request" || (candidate.scope == "instance" && candidate.dimension == "files")
	source := sec030WholeSeed(t, []byte{0x73})
	source.Name, source.ContentType = "synthetic-source.png", "image/png"
	if crossOwner {
		source.UserId = otherOwner
	}
	if candidate.scope == "request" {
		parent := models.FileRequest{Id: rand.Text(), UserId: source.UserId, ApiKey: rand.Text(), MaxFiles: 1000, MaxSize: 1}
		if candidate.dimension == "files" {
			parent.MaxFiles = 2
			// Management copies keep their existing semantics: an expired
			// public upload capability does not become a management upload gate.
			parent.Expiry = time.Now().Add(-time.Hour).Unix()
		}
		database.SaveFileRequest(parent)
		source.UploadRequestId = parent.Id
	}
	storage.AddHotlink(&source)
	database.SaveMetaData(source)
	if source.HotlinkId == "" {
		t.Fatal("CHUNK_QUOTA_DUP_SETUP source has no hotlink")
	}
	hasHold := candidate.scope == "user" && candidate.dimension == "bytes"
	if candidate.dimension == "bytes" {
		length := (1 << 20) - 2
		if hasHold {
			length--
		}
		filler := sec030WholeSeed(t, bytes.Repeat([]byte{0x66}, length))
		if candidate.scope != "user" {
			filler.UserId = otherOwner
		}
		filler.UploadRequestId = source.UploadRequestId
		database.SaveMetaData(filler)
	}
	var hold uploadquota.Reservation
	if hasHold {
		now := time.Now().Unix()
		var err error
		hold, err = database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Reserve,
			Upload: uploadquota.Reservation{ID: "duplicate-hold-" + rand.Text(), UserID: sec030WholeOwner, BytesKnown: true, Bytes: 1, StagingBytes: (64 << 10) + 4, ExpiresAt: now + 3600},
			Limits: testRuntimeChunkLimits(t), Now: now})
		if err != nil {
			t.Fatal("CHUNK_QUOTA_DUP_SETUP active reservation failed")
		}
		defer func() {
			if _, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Abort, Upload: hold, Terminal: uploadquota.Aborted, CleanupConfirmed: true, Now: time.Now().Unix()}); err != nil {
				t.Error("CHUNK_QUOTA_DUP_CLEANUP unused synthetic reservation could not be released")
			}
		}()
	}
	initial := sec030FinishSnapshot(t)
	usage, err := uploadquota.Measure(initial.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_DUP_SETUP invalid synthetic metadata")
	}
	used := usage.Users[sec030WholeOwner]
	if candidate.scope == "request" {
		used = usage.Requests[source.UploadRequestId]
	}
	if candidate.scope == "instance" {
		used = uploadquota.Usage{}
		for _, value := range usage.Users {
			used.Bytes += value.Bytes
			used.Files += value.Files
		}
	}
	if candidate.dimension == "bytes" && used.Bytes+hold.Bytes != (1<<20)-1 || candidate.dimension == "files" && used.Files != 1 {
		t.Fatal("CHUNK_QUOTA_DUP_SETUP fixture does not leave exactly one copy of capacity")
	}
	keys := make([]models.ApiKey, candidate.count)
	for i := range keys {
		keys[i] = sec030WholeKey(t)
		stored, found := database.GetApiKey(keys[i].Id)
		if !found || stored.PublicId == "" || stored.UserId != sec030WholeOwner || !stored.HasPermission(models.ApiPermUpload) {
			t.Fatal("CHUNK_QUOTA_DUP_SETUP distinct management credentials did not persist")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(Process))
	defer server.Close()
	client := server.Client()
	client.Timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if crossOwner {
		denied := sec030DuplicateSend(ctx, client, server.URL, keys[0].Id, source.Id)
		if denied.failed || denied.status != http.StatusUnauthorized || denied.ErrorCode != 6 || !reflect.DeepEqual(initial, sec030FinishSnapshot(t)) {
			t.Fatal("CHUNK_QUOTA_DUP_AUTH foreign source without current list permission was not rejected without effects")
		}
		caller, found := database.GetUser(sec030WholeOwner)
		if !found {
			t.Fatal("CHUNK_QUOTA_DUP_SETUP current caller missing")
		}
		caller.GrantPermission(models.UserPermListOtherUploads)
		database.SaveUser(caller, false)
	}
	// This barrier is before real HTTP calls only; it neither snapshots policy
	// inside the server nor rejects legitimate simultaneous requests as busy.
	start := make(chan struct{})
	ready := make(chan struct{}, candidate.count)
	responses := make([]sec030DuplicateResponse, candidate.count)
	uploadScope := chunksession.Scope{ClientID: "duplicate-vs-upload-" + rand.Text(), UserID: sec030WholeOwner}
	var uploadCall sec030ChunkCall
	if hasHold {
		uploadCall = sec030ChunkAddCall(t, "ordinary", "", keys[candidate.count-1].Id, uploadScope.ClientID, 1, 0, []byte{0x75})
		defer func() {
			if err := storage.ChunkSessions().Cancel(context.Background(), uploadScope); err != nil {
				t.Error("CHUNK_QUOTA_DUP_CLEANUP synthetic competing chunk cleanup failed")
			}
		}()
	}
	var workers sync.WaitGroup
	for i := range responses {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ready <- struct{}{}
			<-start
			if hasHold && i == candidate.count-1 {
				response := sec030ChunkSend(ctx, client, server.URL, uploadCall)
				responses[i] = sec030DuplicateResponse{status: response.status, failed: response.failed, ErrorCode: response.ErrorCode, upload: true, uploadOK: response.Result == "OK"}
			} else {
				responses[i] = sec030DuplicateSend(ctx, client, server.URL, keys[i].Id, source.Id)
			}
		}()
	}
	for range responses {
		<-ready
	}
	close(start)
	workers.Wait()
	accepted := make(map[string]bool)
	quotaDenied := 0
	uploadWon := false
	for _, response := range responses {
		if response.failed {
			t.Error("CHUNK_QUOTA_DUP_TRANSPORT bounded request or JSON response failed")
			continue
		}
		if response.status == http.StatusOK && ((response.upload && response.uploadOK) || (!response.upload && response.Id != "")) {
			if response.upload {
				uploadWon = true
			} else {
				if accepted[response.Id] {
					t.Error("CHUNK_QUOTA_DUP_ID independent calls reused metadata identity")
				}
				accepted[response.Id] = true
			}
		} else if response.status == http.StatusRequestEntityTooLarge && response.ErrorCode == sec030WholeQuotaCode {
			quotaDenied++
		} else {
			t.Errorf("CHUNK_QUOTA_DUP_RESPONSE unexpected status=%d code=%d; quota must be 413/21, not authorization or backpressure", response.status, response.ErrorCode)
		}
	}
	uploadCount := 0
	if uploadWon {
		uploadCount = 1
	}
	if len(accepted)+uploadCount != 1 || quotaDenied != candidate.count-1 {
		t.Errorf("CHUNK_QUOTA_DUP_CAPACITY copies=%d uploads=%d quota_denied=%d; want total_winners=1 denied=%d", len(accepted), uploadCount, quotaDenied, candidate.count-1)
	}
	after := sec030FinishSnapshot(t)
	if len(after.state.metadata) != len(initial.state.metadata)+len(accepted) || len(after.hotlinks) != len(initial.hotlinks)+len(accepted) {
		t.Error("CHUNK_QUOTA_DUP_EFFECTS rejected calls created metadata/hotlinks or changed shared blob bytes")
	}
	for path, data := range initial.state.payloads {
		if !bytes.Equal(data, after.state.payloads[path]) {
			t.Error("CHUNK_QUOTA_DUP_BLOB canonical bytes changed")
		}
	}
	if !uploadWon && !reflect.DeepEqual(initial.state.payloads, after.state.payloads) {
		t.Error("CHUNK_QUOTA_DUP_EFFECTS rejected requests left unexpected staged payload")
	}
	if hasHold {
		if len(after.state.ledger.Records) != len(initial.state.ledger.Records)+uploadCount {
			t.Error("CHUNK_QUOTA_DUP_MIXED copy winner created upload state or refused upload persisted a reservation")
		}
		record, found := after.state.ledger.Records[chunksession.ID(uploadScope)]
		if found != uploadWon || (uploadWon && (!record.BytesKnown || record.Bytes != 1 || record.UserID != sec030WholeOwner || record.RequestID != "" || record.State != uploadquota.Uploading || record.Completing || record.StagingReleased || !sec030ChunkHasPayload(after.state.payloads, []byte{0x75}))) {
			t.Error("CHUNK_QUOTA_DUP_MIXED upload winner lacks its exact scoped active declaration/payload")
		}
	}
	for id, old := range initial.state.metadata {
		if !reflect.DeepEqual(old, after.state.metadata[id]) {
			t.Error("CHUNK_QUOTA_DUP_SOURCE existing metadata changed")
		}
	}
	for id, file := range after.state.metadata {
		if _, existed := initial.state.metadata[id]; existed {
			continue
		}
		if !accepted[id] || file.UserId != sec030WholeOwner || file.UploadRequestId != source.UploadRequestId || file.SizeBytes != source.SizeBytes || file.SHA1 != source.SHA1 || file.AwsBucket != source.AwsBucket {
			t.Error("CHUNK_QUOTA_DUP_SCOPE copy lost current caller, original request, logical size, shared locator or successful response")
		}
		if file.HotlinkId == source.HotlinkId || after.hotlinks[file.HotlinkId] != id {
			t.Error("CHUNK_QUOTA_DUP_HOTLINK mapping missing, reused or points to another file")
		}
	}
	for _, id := range after.hotlinks {
		if _, found := after.state.metadata[id]; !found {
			t.Error("CHUNK_QUOTA_DUP_HOTLINK orphan mapping created")
		}
	}
	measured, err := uploadquota.Measure(after.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_DUP_USAGE invalid post-copy metadata")
	}
	want := usage.Users[sec030WholeOwner]
	want.Bytes += int64(len(accepted)) * source.SizeBytes
	want.Files += int64(len(accepted))
	if measured.Users[sec030WholeOwner] != want {
		t.Error("CHUNK_QUOTA_DUP_USAGE shared blob was not charged for each caller-owned metadata record")
	}
	if source.UploadRequestId != "" {
		want = usage.Requests[source.UploadRequestId]
		want.Bytes += int64(len(accepted)) * source.SizeBytes
		want.Files += int64(len(accepted))
		if measured.Requests[source.UploadRequestId] != want {
			t.Error("CHUNK_QUOTA_DUP_REQUEST original request accounting was detached by cross-owner copy")
		}
	}
	// A later refusal must leave metadata, hotlinks and physical bytes unchanged.
	retrySource := source.Id
	if source.UploadRequestId != "" {
		// A cross-owner copy retains its parent request. Copying that result
		// must reach capacity admission rather than an invalid owner conflict.
		for id := range accepted {
			retrySource = id
		}
	}
	refused := sec030DuplicateSend(ctx, client, server.URL, keys[0].Id, retrySource)
	final := sec030FinishSnapshot(t)
	if refused.failed || refused.status != http.StatusRequestEntityTooLarge || refused.ErrorCode != sec030WholeQuotaCode || !reflect.DeepEqual(after.state.metadata, final.state.metadata) || !reflect.DeepEqual(after.hotlinks, final.hotlinks) || !reflect.DeepEqual(after.state.payloads, final.state.payloads) {
		t.Error("CHUNK_QUOTA_DUP_REFUSAL full-capacity retry did not refuse as 413/21 without metadata/hotlink/blob effects")
	}
	if hasHold && !reflect.DeepEqual(hold, final.state.ledger.Records[hold.ID]) {
		t.Error("CHUNK_QUOTA_DUP_RESERVATION copy consumed or changed another active upload declaration")
	}
}

type sec030DuplicateResponse struct {
	models.FileApiOutput
	ErrorCode int
	status    int
	failed    bool
	upload    bool
	uploadOK  bool
}

func sec030DuplicateSend(ctx context.Context, client *http.Client, baseURL, key, id string) sec030DuplicateResponse {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/files/duplicate", nil)
	if err != nil {
		return sec030DuplicateResponse{failed: true}
	}
	request.Header.Set("apikey", key)
	request.Header.Set("id", id)
	response, err := client.Do(request)
	if err != nil {
		return sec030DuplicateResponse{failed: true}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	result := sec030DuplicateResponse{status: response.StatusCode}
	if err != nil || len(data) > 16<<10 || json.Unmarshal(data, &result) != nil {
		result.failed = true
	}
	return result
}
