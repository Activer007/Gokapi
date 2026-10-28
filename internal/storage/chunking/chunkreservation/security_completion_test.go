package chunkreservation

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestSEC032CompletionReservation(t *testing.T) {
	for _, state := range []string{"own", "other-request", "missing", "expired", "expiry-boundary"} {
		t.Run(state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				resetStateBlockCleanup()
				id, uuid := "synthetic-owner", New("synthetic-owner")
				if state == "expired" || state == "expiry-boundary" {
					value := reservedChunks[id][uuid]
					value.Expiry = time.Now().Unix()
					if state == "expired" {
						value.Expiry--
					}
					reservedChunks[id][uuid] = value
				}
				before := reservedChunks[id][uuid]
				if state == "other-request" {
					id = "synthetic-other"
				}
				if state == "missing" {
					uuid = "synthetic-missing"
				}
				if IsValid(id, uuid) != (state == "own") {
					t.Error("completion reservation scope/lifetime mismatch")
				}
				if GetCount("synthetic-owner") != 1 {
					t.Error("validation consumed a reservation")
				}
				for _, after := range reservedChunks["synthetic-owner"] {
					if before != after {
						t.Error("validation changed reservation/lifetime")
					}
				}
				if state != "own" && SetUploading(id, uuid) {
					t.Error("add accepted a reservation rejected by completion")
				}
			})
		})
	}
}
