//go:build test && security_whole && security_replace && !integration

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

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/storage/chunking/chunksession"
	"github.com/forceu/gokapi/internal/uploadquota"
)

// Every replacement has its own 1B target. Replaying one target would turn its
// later replacements into zero-delta successes and could hide missing admission.
// Most 2B sources have distinct blobs so their publication locks do not serialize
// admission. Two source IDs share one hash to retain alias coverage as well.
func TestSEC030ReplaceQuota(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, item := range []struct {
				scope string
				count int
			}{{"user", 10}, {"instance", 50}} {
				candidate := sec030ChunkCase{backend, "replace", item.scope, "bytes", item.count}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ReplaceQuota", candidate) })
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	if candidate.kind != "replace" || candidate.dimension != "bytes" ||
		(candidate.scope != "user" && candidate.scope != "instance") ||
		(candidate.scope == "user" && candidate.count != 10) ||
		(candidate.scope == "instance" && candidate.count != 50) {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP invalid isolated case")
	}
	sec030WholeSetup(t, candidate.backend)
	sec030ReplaceActor(t)
	targets := make([]models.File, candidate.count-1)
	sources := make([]models.File, len(targets))
	keys := make([]models.ApiKey, len(targets))
	for i := range targets {
		payload := []byte{0x73, byte(i)}
		if i == 1 {
			payload[1] = 0 // Two distinct source IDs intentionally alias one blob.
		}
		source := sec030WholeSeed(t, payload)
		source.Name, source.Size, source.ContentType = "synthetic-new-content.png", "2 B", "image/png"
		source.Encryption = models.EncryptionInfo{IsEncrypted: true, DecryptionKey: []byte{0x6b}, Nonce: []byte{0x6e}}
		storage.AddHotlink(&source)
		database.SaveMetaData(source)
		if source.HotlinkId == "" {
			t.Fatal("CHUNK_QUOTA_REPLACE_SETUP source has no hotlink")
		}
		sources[i] = source
		target := sec030WholeSeed(t, []byte{0x74})
		target.Name, target.Size, target.ContentType = "synthetic-existing.txt", "1 B", "text/plain"
		target.PasswordHash, target.UploadDate, target.DownloadCount = "synthetic-existing-password-hash", 1234567890, 7
		target.UnlimitedTime, target.ExpireAt = false, time.Now().Add(time.Hour).Unix()
		target.UnlimitedDownloads, target.DownloadsRemaining = false, 17
		target.Encryption = models.EncryptionInfo{IsEncrypted: true, DecryptionKey: []byte{0x61}, Nonce: []byte{0x62}}
		database.SaveMetaData(target)
		targets[i], keys[i] = target, sec030ReplaceKey(t, models.ApiPermReplace)
	}
	// Completed bytes + this existing hold leave just 1B. Instance contention
	// uses a foreign filler so the receiving user's own byte limit stays roomy.
	beforeFiller, err := uploadquota.Measure(database.GetAllMetadata())
	if err != nil {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP invalid source or target metadata")
	}
	fillerUsage := beforeFiller.Users[sec030WholeOwner]
	if candidate.scope == "instance" {
		fillerUsage = sec030ReplaceInstance(beforeFiller)
	}
	fillerBytes := (1 << 20) - 2 - fillerUsage.Bytes
	if fillerBytes <= 0 || fillerBytes > 1<<20 {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP invalid remaining filler capacity")
	}
	filler := sec030WholeSeed(t, bytes.Repeat([]byte{0x66}, int(fillerBytes)))
	if candidate.scope == "instance" {
		const otherOwner = sec030WholeOwner + 1
		database.SaveUser(models.User{Id: otherOwner, Name: "synthetic-replace-filler", UserLevel: models.UserLevelUser}, false)
		filler.UserId = otherOwner
		database.SaveMetaData(filler)
	}
	now := time.Now().Unix()
	hold, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Reserve,
		Upload: uploadquota.Reservation{ID: "replace-hold-" + rand.Text(), UserID: sec030WholeOwner, BytesKnown: true, Bytes: 1, StagingBytes: (64 << 10) + 4, ExpiresAt: now + 3600},
		Limits: testRuntimeChunkLimits(t), Now: now})
	if err != nil {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP active reservation failed")
	}
	defer func() {
		if _, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Abort, Upload: hold, Terminal: uploadquota.Aborted, CleanupConfirmed: true, Now: time.Now().Unix()}); err != nil {
			t.Error("CHUNK_QUOTA_REPLACE_CLEANUP synthetic hold could not be released")
		}
	}()
	initial := sec030FinishSnapshot(t)
	usage, err := uploadquota.Measure(initial.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP invalid initial metadata")
	}
	used := usage.Users[sec030WholeOwner]
	if candidate.scope == "instance" {
		used = sec030ReplaceInstance(usage)
	}
	if used.Bytes+hold.Bytes != (1<<20)-1 {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP contested scope must have exactly one byte free")
	}
	server := httptest.NewServer(http.HandlerFunc(Process))
	defer server.Close()
	client := server.Client()
	client.Timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	uploadScope := chunksession.Scope{ClientID: "replace-vs-upload-" + rand.Text(), UserID: sec030WholeOwner}
	uploadKey := sec030WholeKey(t)
	uploadCall := sec030ChunkAddCall(t, "ordinary", "", uploadKey.Id, uploadScope.ClientID, 1, 0, []byte{0x75})
	defer func() {
		if err := storage.ChunkSessions().Cancel(context.Background(), uploadScope); err != nil {
			t.Error("CHUNK_QUOTA_REPLACE_CLEANUP competing upload could not be released")
		}
	}()
	start, ready := make(chan struct{}), make(chan struct{}, candidate.count)
	responses := make([]sec030ReplaceResponse, candidate.count)
	var workers sync.WaitGroup
	for i := range responses {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			ready <- struct{}{}
			<-start
			if i == len(targets) {
				response := sec030ChunkSend(ctx, client, server.URL, uploadCall)
				responses[i] = sec030ReplaceResponse{status: response.status, failed: response.failed, ErrorCode: response.ErrorCode, Result: response.Result, ErrorMessage: response.ErrorMessage}
				return
			}
			responses[i] = sec030ReplaceSend(ctx, client, server.URL, keys[i].Id, targets[i].Id, sources[i].Id, false)
		}(i)
	}
	for range responses {
		<-ready
	}
	close(start)
	workers.Wait()
	accepted := make(map[string]int)
	quotaDenied, uploadCount := 0, 0
	for i, response := range responses {
		if response.failed {
			t.Error("CHUNK_QUOTA_REPLACE_TRANSPORT bounded request or JSON response failed")
			continue
		}
		if response.status == http.StatusOK && ((i == len(targets) && response.Result == "OK") || (i < len(targets) && response.Id == targets[i].Id)) {
			if i == len(targets) {
				uploadCount++
			} else {
				accepted[response.Id] = i
			}
		} else if response.status == http.StatusRequestEntityTooLarge && response.ErrorCode == sec030WholeQuotaCode && response.Result == "error" && response.ErrorMessage != "" {
			quotaDenied++
		} else {
			t.Errorf("CHUNK_QUOTA_REPLACE_RESPONSE unexpected status=%d code=%d; quota must be 413/21, not authorization or backpressure", response.status, response.ErrorCode)
		}
	}
	if len(accepted)+uploadCount != 1 || quotaDenied != candidate.count-1 {
		t.Errorf("CHUNK_QUOTA_REPLACE_CAPACITY replacements=%d uploads=%d quota_denied=%d; want total_winners=1 denied=%d", len(accepted), uploadCount, quotaDenied, candidate.count-1)
	}
	after := sec030FinishSnapshot(t)
	if len(after.state.metadata) != len(initial.state.metadata) || !reflect.DeepEqual(initial.hotlinks, after.hotlinks) {
		t.Error("CHUNK_QUOTA_REPLACE_EFFECTS replacement changed metadata count or hotlink mappings")
	}
	for id, old := range initial.state.metadata {
		if index, replaced := accepted[id]; replaced {
			source := sources[index]
			old.Name, old.Size, old.SHA1 = source.Name, source.Size, source.SHA1
			old.ContentType, old.AwsBucket = source.ContentType, source.AwsBucket
			old.SizeBytes, old.Encryption = source.SizeBytes, source.Encryption
		}
		if !reflect.DeepEqual(old, after.state.metadata[id]) {
			t.Error("CHUNK_QUOTA_REPLACE_METADATA only accepted target content fields may change; source and target policies must survive")
		}
	}
	for path, data := range initial.state.payloads {
		if !bytes.Equal(data, after.state.payloads[path]) {
			t.Error("CHUNK_QUOTA_REPLACE_BLOB canonical bytes changed")
		}
	}
	if uploadCount == 0 && !reflect.DeepEqual(initial.state.payloads, after.state.payloads) {
		t.Error("CHUNK_QUOTA_REPLACE_EFFECTS refused requests left staged payloads")
	}
	if len(after.state.ledger.Records) != len(initial.state.ledger.Records)+uploadCount || after.state.ledger.Version != initial.state.ledger.Version {
		t.Error("CHUNK_QUOTA_REPLACE_LEDGER replacement created a receipt or refused upload retained a reservation")
	}
	for id, record := range initial.state.ledger.Records {
		if !reflect.DeepEqual(record, after.state.ledger.Records[id]) {
			t.Error("CHUNK_QUOTA_REPLACE_LEDGER replacement consumed or changed the existing hold")
		}
	}
	record, found := after.state.ledger.Records[chunksession.ID(uploadScope)]
	if found != (uploadCount == 1) || (found && (!record.BytesKnown || record.Bytes != 1 || record.UserID != sec030WholeOwner || record.RequestID != "" || record.State != uploadquota.Uploading || record.Completing || record.StagingReleased || !sec030ChunkHasPayload(after.state.payloads, []byte{0x75}))) {
		t.Error("CHUNK_QUOTA_REPLACE_MIXED upload winner lacks its exact scoped declaration and payload")
	}
	measured, err := uploadquota.Measure(after.state.metadata)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_REPLACE_USAGE invalid post-replacement metadata")
	}
	want := usage.Users[sec030WholeOwner]
	want.Bytes += int64(len(accepted))
	instanceWant := sec030ReplaceInstance(usage)
	instanceWant.Bytes += int64(len(accepted))
	if measured.Users[sec030WholeOwner] != want || sec030ReplaceInstance(measured) != instanceWant || !reflect.DeepEqual(usage.Requests, measured.Requests) {
		t.Error("CHUNK_QUOTA_REPLACE_USAGE only receiving owner and instance bytes may grow; file and original request counts must stay fixed")
	}
	for i, target := range targets {
		if _, replaced := accepted[target.Id]; replaced {
			continue
		}
		refused := sec030ReplaceSend(ctx, client, server.URL, keys[i].Id, target.Id, sources[i].Id, false)
		if refused.failed || refused.status != http.StatusRequestEntityTooLarge || refused.ErrorCode != sec030WholeQuotaCode || !reflect.DeepEqual(after, sec030FinishSnapshot(t)) {
			t.Error("CHUNK_QUOTA_REPLACE_REFUSAL full-capacity retry did not refuse as 413/21 without effects")
		}
		break
	}
}

// A self replacement is harmless without deletion; deleting that same identity
// must conflict. Its own temporary child contains the old asynchronous cleanup.
func TestSEC030ReplaceSelfDeleteConflicts(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		sec030ChunkRunChild(t, "TestSEC030ReplaceSelfDeleteConflicts", sec030ChunkCase{"sqlite", "replace", "user", "self", 1})
		return
	}
	candidate := sec030ChunkSelected(t)
	if candidate.kind != "replace" || candidate.dimension != "self" || candidate.backend != "sqlite" || candidate.scope != "user" || candidate.count != 1 {
		t.Fatal("CHUNK_QUOTA_REPLACE_SELF_SETUP invalid isolated case")
	}
	sec030WholeSetup(t, candidate.backend)
	sec030ReplaceActor(t)
	file := sec030WholeSeed(t, []byte{0x73})
	key := sec030ReplaceKey(t, models.ApiPermReplace|models.ApiPermDelete)
	server := httptest.NewServer(http.HandlerFunc(Process))
	defer server.Close()
	client := server.Client()
	client.Timeout = 15 * time.Second
	before := sec030FinishSnapshot(t)
	response := sec030ReplaceSend(context.Background(), client, server.URL, key.Id, file.Id, file.Id, false)
	if response.failed || response.status != http.StatusOK || response.Id != file.Id || !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
		t.Fatal("CHUNK_QUOTA_REPLACE_SELF_NOOP same-file replacement without deletion was not an unchanged success")
	}
	response = sec030ReplaceSend(context.Background(), client, server.URL, key.Id, file.Id, file.Id, true)
	if response.failed || response.status != http.StatusConflict || response.ErrorCode != 19 {
		t.Fatal("CHUNK_QUOTA_REPLACE_SELF_DELETE same-file deletion must conflict as 409/19")
	}
	if !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
		t.Error("CHUNK_QUOTA_REPLACE_SELF_DELETE conflicting same-file deletion changed durable state")
	}
}

func sec030ReplaceActor(t *testing.T) {
	t.Helper()
	actor, found := database.GetUser(sec030WholeOwner)
	if !found || actor.UserLevel != models.UserLevelUser {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP missing ordinary management caller")
	}
	actor.GrantPermission(models.UserPermReplaceUploads)
	database.SaveUser(actor, false)
}

func sec030ReplaceKey(t *testing.T, permissions models.ApiPermission) models.ApiKey {
	t.Helper()
	key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: sec030WholeOwner, Permissions: permissions}
	database.SaveApiKey(key)
	stored, found := database.GetApiKey(key.Id)
	if !found || stored.UserId != sec030WholeOwner || stored.PublicId == "" || !stored.HasPermission(permissions) {
		t.Fatal("CHUNK_QUOTA_REPLACE_SETUP distinct authorized credentials did not persist")
	}
	return key
}

func sec030ReplaceInstance(snapshot uploadquota.Snapshot) uploadquota.Usage {
	var result uploadquota.Usage
	for _, usage := range snapshot.Users {
		result.Bytes += usage.Bytes
		result.Files += usage.Files
	}
	return result
}

type sec030ReplaceResponse struct {
	models.FileApiOutput
	Result       string
	ErrorCode    int
	ErrorMessage string
	status       int
	failed       bool
}

func sec030ReplaceSend(ctx context.Context, client *http.Client, baseURL, key, id, newID string, deleteSource bool) sec030ReplaceResponse {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/files/replace", nil)
	if err != nil {
		return sec030ReplaceResponse{failed: true}
	}
	request.Header.Set("apikey", key)
	request.Header.Set("id", id)
	request.Header.Set("idNewContent", newID)
	request.Header.Set("deleteNewFile", strconv.FormatBool(deleteSource))
	response, err := client.Do(request)
	if err != nil {
		return sec030ReplaceResponse{failed: true}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	result := sec030ReplaceResponse{status: response.StatusCode}
	if err != nil || len(data) > 16<<10 || json.Unmarshal(data, &result) != nil {
		result.failed = true
	}
	return result
}
