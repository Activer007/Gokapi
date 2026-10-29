package pstatusdb

import (
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestSEC030ChunkCommittedStatusRecovery(t *testing.T) {
	// Keep the fixture isolated from existing tests that count the whole map.
	const owner = 901
	const id = "verified-receipt-recovery"
	key := uploadKey{userId: owner, chunkId: id}
	t.Cleanup(func() { statusMutex.Lock(); delete(statusMap, key); statusMutex.Unlock() })
	failed := models.UploadStatus{UserId: owner, ChunkId: id, CurrentStatus: 3, ErrorMessage: "retry required"}
	Set(failed)
	finished := models.UploadStatus{UserId: owner, ChunkId: id, CurrentStatus: 2, FileId: "committed-file"}
	Set(finished)
	if statuses := GetAllForUser(owner); len(statuses) != 1 || statuses[0].CurrentStatus != 3 {
		t.Fatal("ordinary status updates lost the monotonic error guard")
	}
	for _, invalid := range []models.UploadStatus{
		{UserId: owner, ChunkId: id, CurrentStatus: 2},
		{UserId: owner, ChunkId: id, CurrentStatus: 3, FileId: "unverified"},
		{UserId: owner, ChunkId: id, CurrentStatus: 2, FileId: "unverified", ErrorMessage: "still failed"},
	} {
		SetCommitted(invalid)
		if statuses := GetAllForUser(owner); len(statuses) != 1 || statuses[0].CurrentStatus != 3 {
			t.Fatal("invalid receipt status replaced the prior error")
		}
	}
	SetCommitted(finished)
	statuses := GetAllForUser(owner)
	if len(statuses) != 1 || statuses[0].CurrentStatus != 2 || statuses[0].FileId != finished.FileId || statuses[0].ErrorMessage != "" {
		t.Fatal("verified receipt did not replace the earlier error")
	}
}
