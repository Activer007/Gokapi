//go:build test

package pstatusdb

import (
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestSEC032StatusScope(t *testing.T) {
	const id = "sec032-same-client-uuid"
	t.Cleanup(func() {
		statusMutex.Lock()
		defer statusMutex.Unlock()
		for key, status := range statusMap {
			if status.ChunkId == id {
				delete(statusMap, key)
			}
		}
	})
	Set(models.UploadStatus{UserId: 9132, ChunkId: id, CurrentStatus: 2, FileId: "synthetic-a"})
	Set(models.UploadStatus{UserId: 9133, ChunkId: id, CurrentStatus: 0})
	for user, expected := range map[int]int{9132: 2, 9133: 0} {
		count := 0
		for _, status := range GetAllForUser(user) {
			if status.ChunkId != id {
				continue
			}
			count++
			if status.UserId != user || status.CurrentStatus != expected {
				t.Error("upload status crossed user boundary")
			}
		}
		if count != 1 {
			t.Errorf("user %d has %d statuses for same client UUID, want 1", user, count)
		}
	}
}
