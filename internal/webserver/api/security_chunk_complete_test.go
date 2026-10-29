//go:build test && security_whole && !integration

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

// Completion is exercised only through public HTTP routes. No test requires a
// new runtime symbol, so the historical baseline can compile and reach RED.
func TestSEC030ChunkCompleteCoverage(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				candidate := sec030ChunkCase{backend, kind, "user", "finish", 1}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkCompleteCoverage", candidate) })
			}
		}
		return
	}
	fixture := sec030FinishSetup(t, sec030ChunkSelected(t))
	tail := fixture.add(t, 1<<20, fixture.content[1<<20:])
	if response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, tail); !sec030ChunkOK(response) {
		t.Fatalf("CHUNK_QUOTA_FINISH_SETUP valid tail-first upload failed status=%d code=%d", response.status, response.ErrorCode)
	}
	before := sec030FinishSnapshot(t)
	response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(false))
	sec030FinishDenied(t, response)
	if !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
		t.Fatal("CHUNK_QUOTA_FINISH_COVERAGE missing-range completion changed payload, metadata, hotlinks or ledger")
	}
	// The missing head and an identical tail retry arrive together. Their final
	// bytes must be independent of scheduling and of the earlier refused finish.
	responses := sec030ChunkBatch(t, fixture.client, fixture.url, fixture.gate, []sec030ChunkCall{fixture.add(t, 0, fixture.content[:1<<20]), tail})
	for _, response := range responses {
		if !sec030ChunkOK(response) {
			t.Fatalf("CHUNK_QUOTA_FINISH_SETUP parallel coverage repair failed status=%d code=%d", response.status, response.ErrorCode)
		}
	}
	response = sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(false))
	if !sec030ChunkOK(response) || response.FileInfo.Id == "" {
		t.Fatalf("CHUNK_QUOTA_FINISH_RESPONSE complete upload did not return its file status=%d code=%d", response.status, response.ErrorCode)
	}
	file := sec030FinishSettled(t, fixture)
	if response.FileInfo.Id != file.Id {
		t.Error("CHUNK_QUOTA_FINISH_ID synchronous response differs from the committed file")
	}
	sec030FinishCheckFile(t, fixture, file)
}

func TestSEC030ChunkCompleteContenders(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				for _, mode := range []string{"sync", "async"} {
					for _, count := range []int{10, 50} {
						candidate := sec030ChunkCase{backend, kind, mode, "finish", count}
						t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkCompleteContenders", candidate) })
					}
				}
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	fixture := sec030FinishSetup(t, candidate)
	sec030FinishUpload(t, fixture)
	asynchronous := candidate.scope == "async"
	calls := make([]sec030ChunkCall, candidate.count)
	for i := range calls {
		calls[i] = fixture.complete(asynchronous)
	}
	responses := sec030ChunkBatch(t, fixture.client, fixture.url, fixture.gate, calls)
	accepted := 0
	ids := make(map[string]bool)
	for _, response := range responses {
		if sec030ChunkOK(response) {
			accepted++
			if !asynchronous && response.FileInfo.Id == "" {
				t.Error("CHUNK_QUOTA_FINISH_ID synchronous success omitted the committed identity")
			}
			if response.FileInfo.Id != "" {
				ids[response.FileInfo.Id] = true
			}
		} else if response.failed || response.status != http.StatusConflict || response.Result != "error" || response.FileInfo.Id != "" {
			t.Errorf("CHUNK_QUOTA_FINISH_CONTENTION duplicate completion status=%d code=%d; want success or conflict", response.status, response.ErrorCode)
		}
	}
	if accepted == 0 || len(ids) > 1 {
		t.Errorf("CHUNK_QUOTA_FINISH_ID accepted=%d distinct synchronous identities=%d; want success and at most one identity", accepted, len(ids))
	}
	file := sec030FinishSettled(t, fixture)
	sec030FinishCheckFile(t, fixture, file)
	for id := range ids {
		if id != file.Id {
			t.Error("CHUNK_QUOTA_FINISH_ID one UUID returned an unrelated identity")
		}
	}
	before := sec030FinishSnapshot(t)
	for _, detached := range []bool{false, true} {
		response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(detached))
		if !sec030ChunkOK(response) || (!detached && response.FileInfo.Id != file.Id) || (response.FileInfo.Id != "" && response.FileInfo.Id != file.Id) {
			t.Errorf("CHUNK_QUOTA_FINISH_REPLAY committed replay did not return its original identity/ACK status=%d code=%d", response.status, response.ErrorCode)
		}
		if !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
			t.Error("CHUNK_QUOTA_FINISH_REPLAY committed replay changed persistent state")
		}
	}
}

func TestSEC030ChunkCompleteReplayAuthority(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				candidate := sec030ChunkCase{backend, kind, "user", "finish", 1}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkCompleteReplayAuthority", candidate) })
			}
		}
		return
	}
	fixture := sec030FinishSetup(t, sec030ChunkSelected(t))
	sec030FinishUpload(t, fixture)
	if response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(false)); !sec030ChunkOK(response) {
		t.Fatalf("CHUNK_QUOTA_FINISH_SETUP initial completion failed status=%d code=%d", response.status, response.ErrorCode)
	}
	file := sec030FinishSettled(t, fixture)
	other := models.User{Id: sec030WholeOwner + 1, Name: "synthetic-other-owner", UserLevel: models.UserLevelUser}
	database.SaveUser(other, false)
	for _, mutation := range []string{"transferred", "deleted"} {
		if mutation == "transferred" {
			moved := file
			moved.UserId, moved.Name = other.Id, "synthetic-other-owner.png"
			database.SaveMetaData(moved)
		} else {
			database.DeleteMetaData(file.Id)
		}
		before := sec030FinishSnapshot(t)
		for _, detached := range []bool{false, true} {
			response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(detached))
			sec030FinishDenied(t, response)
			if !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
				t.Errorf("CHUNK_QUOTA_FINISH_AUTHORITY replay after %s recreated or changed persistent state", mutation)
			}
		}
	}
}

func TestSEC030ChunkCompleteCurrentRequestPolicy(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, policy := range []string{"finish-size", "finish-expiry"} {
				candidate := sec030ChunkCase{backend, "fr", "request", policy, 1}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkCompleteCurrentRequestPolicy", candidate) })
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	fixture := sec030FinishSetup(t, candidate)
	sec030FinishUpload(t, fixture)
	current := fixture.request
	if candidate.dimension == "finish-size" {
		current.MaxSize = 1
	} else {
		current.Expiry = time.Now().Add(-time.Minute).Unix()
	}
	database.SaveFileRequest(current)
	if stored, found := database.GetFileRequest(current.Id); !found || stored.MaxSize != current.MaxSize || stored.Expiry != current.Expiry {
		t.Fatal("CHUNK_QUOTA_FINISH_SETUP current request policy did not persist")
	}
	before := sec030FinishSnapshot(t)
	for _, detached := range []bool{false, true} {
		response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.complete(detached))
		// Expired credentials and a reduced per-file maximum retain their own
		// public error identities; only refusal and unchanged state are common.
		sec030FinishDenied(t, response)
		if !reflect.DeepEqual(before, sec030FinishSnapshot(t)) {
			t.Error("CHUNK_QUOTA_FINISH_POLICY rejected completion claimed, published or refunded an upload")
		}
	}
}

type sec030FinishFixture struct {
	candidate sec030ChunkCase
	request   models.FileRequest
	key       models.ApiKey
	uuid      string
	url       string
	client    *http.Client
	gate      *sec030ChunkGate
	content   []byte
}

func sec030FinishSetup(t *testing.T, candidate sec030ChunkCase) sec030FinishFixture {
	t.Helper()
	sec030WholeSetup(t, candidate.backend)
	configuration.Get().ChunkSize = 1
	request, key := sec030ChunkRequestScope(t, candidate)
	if candidate.kind == "fr" {
		request.MaxSize = 4 // Preserve the existing per-file layer independently.
		database.SaveFileRequest(request)
	} else {
		key = sec030WholeKey(t)
	}
	if stored, found := database.GetApiKey(key.Id); !found || stored.PublicId == "" || stored.UserId != sec030WholeOwner {
		t.Fatal("CHUNK_QUOTA_FINISH_SETUP authenticated fixture key did not persist")
	}
	server, gate := sec030ChunkServer(t, max(candidate.count, 2))
	client := server.Client()
	client.Timeout = 30 * time.Second
	uuid := rand.Text()
	if candidate.kind == "fr" {
		uuid = sec030ChunkReserveOne(t, client, server.URL, request, key)
	}
	content := append(bytes.Repeat([]byte{0x41}, 1<<20), bytes.Repeat([]byte{0x42}, 1<<20)...)
	return sec030FinishFixture{candidate, request, key, uuid, server.URL, client, gate, content}
}

func (fixture sec030FinishFixture) add(t *testing.T, offset int64, content []byte) sec030ChunkCall {
	return sec030ChunkAddCall(t, fixture.candidate.kind, fixture.request.Id, fixture.key.Id, fixture.uuid, int64(len(fixture.content)), offset, content)
}

func (fixture sec030FinishFixture) complete(detached bool) sec030ChunkCall {
	call := sec030ChunkCall{path: "/chunk/complete", key: fixture.key.Id, uuid: fixture.uuid, headers: map[string]string{
		"uuid": fixture.uuid, "filename": "synthetic-complete.png", "filesize": strconv.Itoa(len(fixture.content)),
		"contenttype": "image/png", "allowedDownloads": "0", "expiryDays": "0", "nonblocking": strconv.FormatBool(detached),
	}}
	if fixture.candidate.kind == "fr" {
		call.path = "/uploadrequest/chunk/complete"
		call.headers["fileRequestId"] = fixture.request.Id
	}
	return call
}

func sec030FinishUpload(t *testing.T, fixture sec030FinishFixture) {
	t.Helper()
	calls := []sec030ChunkCall{fixture.add(t, 1<<20, fixture.content[1<<20:]), fixture.add(t, 0, fixture.content[:1<<20])}
	responses := make([]sec030ChunkResponse, len(calls))
	var workers sync.WaitGroup
	for i := range calls {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			responses[i] = sec030ChunkSend(context.Background(), fixture.client, fixture.url, calls[i])
		}(i)
	}
	workers.Wait()
	for _, response := range responses {
		if !sec030ChunkOK(response) {
			t.Fatalf("CHUNK_QUOTA_FINISH_SETUP valid parallel chunk upload failed status=%d code=%d", response.status, response.ErrorCode)
		}
	}
}

func sec030FinishDenied(t *testing.T, response sec030ChunkResponse) {
	t.Helper()
	if response.failed || response.status < 400 || response.status >= 500 || response.Result != "error" || response.ErrorMessage == "" || response.FileInfo.Id != "" || response.ErrorCode == sec030WholeBusyCode {
		t.Errorf("CHUNK_QUOTA_FINISH_REFUSAL completion must refuse without file metadata status=%d code=%d", response.status, response.ErrorCode)
	}
}

func sec030FinishSettled(t *testing.T, fixture sec030FinishFixture) models.File {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		files := database.GetAllMetadata()
		ledger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal("CHUNK_QUOTA_FINISH_LEDGER cannot read completion receipt")
		}
		if len(files) > 1 || len(ledger.Records) > 1 {
			t.Fatal("CHUNK_QUOTA_FINISH_ID one UUID created multiple files or receipts")
		}
		if len(files) == 1 && len(ledger.Records) == 1 {
			for _, record := range ledger.Records {
				file, found := files[record.FileID]
				if found && record.State == uploadquota.Committed && record.StagingReleased && !record.Completing && record.CommitDigest != "" && record.UserID == sec030WholeOwner && record.RequestID == fixture.request.Id && record.Bytes == int64(len(fixture.content)) {
					return file
				}
			}
		}
		select {
		case <-timer.C:
			t.Fatal("CHUNK_QUOTA_FINISH_LEDGER completion did not settle as one committed receipt with released staging")
		case <-tick.C:
		}
	}
}

func sec030FinishCheckFile(t *testing.T, fixture sec030FinishFixture, file models.File) {
	t.Helper()
	if file.UserId != sec030WholeOwner || file.UploadRequestId != fixture.request.Id || file.SizeBytes != int64(len(fixture.content)) || file.SHA1 == "" {
		t.Error("CHUNK_QUOTA_FINISH_METADATA committed file lost authenticated scope or exact size")
	}
	data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, file.SHA1))
	if err != nil || !bytes.Equal(data, fixture.content) || sha256.Sum256(data) != sha256.Sum256(fixture.content) {
		t.Error("CHUNK_QUOTA_FINISH_CONTENT completed blob differs from the exact out-of-order payload")
	}
	payloads := sec030ChunkPayloads(t)
	if len(payloads) != 1 || !bytes.Equal(payloads[file.SHA1], fixture.content) {
		t.Error("CHUNK_QUOTA_FINISH_CLEANUP released staging still contains chunk manifests, input or processing work")
	}
	hotlinks := database.GetAllHotlinks()
	target, found := database.GetHotlink(file.HotlinkId)
	if file.HotlinkId == "" || len(hotlinks) != 1 || !found || target != file.Id {
		t.Error("CHUNK_QUOTA_FINISH_HOTLINK completion did not create exactly one mapping to the committed file")
	}
}

type sec030FinishState struct {
	state    sec030ChunkState
	hotlinks map[string]string
}

func sec030FinishSnapshot(t *testing.T) sec030FinishState {
	t.Helper()
	result := sec030FinishState{state: sec030ChunkSnapshot(t), hotlinks: make(map[string]string)}
	for _, id := range database.GetAllHotlinks() {
		if target, found := database.GetHotlink(id); found {
			result.hotlinks[id] = target
		} else {
			t.Fatal("CHUNK_QUOTA_FINISH_HOTLINK cannot snapshot an existing mapping")
		}
	}
	return result
}
