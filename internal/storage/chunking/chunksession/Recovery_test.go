//go:build test && security_chunk

package chunksession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func TestSEC030RecoveryChunkVerifyIsReadOnly(t *testing.T) {
	s := testStore(t, "sqlite")
	ctx := context.Background()
	if err := s.VerifyRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(s.path("")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("verification created a private namespace", err)
	}
	scope := testScopes()[0]
	if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	progress := s.path("progress-" + key(ID(scope)) + "-" + strings.Repeat("a", 32))
	if err := os.WriteFile(progress, []byte("owned progress"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := s.read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	foreign := s.path("foreign-entry")
	if err := os.WriteFile(foreign, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	// Verify must inspect every entry even on an already initialized Store.
	if err := s.VerifyRecovery(ctx); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("foreign namespace entry was accepted", err)
	}
	for _, path := range []string{progress, foreign, s.payloadPath(ID(scope)), s.manifestPath(ID(scope))} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("verification removed a private or foreign file", err)
		}
	}
	after, err := s.read(ctx)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("verification changed quota state", err)
	}
}

func TestSEC030RecoveryChunkCommittedInputScope(t *testing.T) {
	for _, scenario := range []string{"committed", "claim", "mismatched-committed"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t, "sqlite")
			ctx := context.Background()
			scope := testScopes()[0]
			r, err := testWrite(s, scope, 3, 0, []byte("abc"))
			if err != nil {
				t.Fatal(err)
			}
			r, err = s.apply(ctx, uploadquota.Change{Action: uploadquota.Claim, Upload: r, FileID: "recovery-file", Limits: testLimits(), Now: s.now()})
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "claim" {
				file := models.File{Id: r.FileID, UserId: r.UserID, SizeBytes: r.Bytes, Name: "recovery.bin", SHA1: "recovery-blob", UnlimitedTime: true, UnlimitedDownloads: true}
				if _, err := s.apply(ctx, uploadquota.Change{Action: uploadquota.Commit, Upload: r, File: &file, Limits: testLimits(), Now: s.now()}); err != nil {
					t.Fatal(err)
				}
			}
			canonical := filepath.Join(s.root, "recovery-blob")
			if err := os.WriteFile(canonical, []byte("canonical"), 0600); err != nil {
				t.Fatal(err)
			}
			expected := testReceipt(t, s, scope)
			if scenario == "mismatched-committed" {
				expected.Bytes++
			}
			before, err := s.read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			restarted := New(s.root)
			restarted.apply, restarted.read = s.apply, s.read
			err = restarted.CleanupCommittedInput(ctx, expected)
			if scenario == "committed" {
				if err != nil {
					t.Fatal(err)
				}
				if err := restarted.CleanupCommittedInput(ctx, expected); err != nil {
					t.Fatal("repeated input cleanup was not idempotent", err)
				}
			} else if !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("unconfirmed receipt was allowed to clean input", err)
			}
			for _, path := range []string{s.payloadPath(r.ID), s.manifestPath(r.ID)} {
				_, err := os.Lstat(path)
				if scenario == "committed" && !errors.Is(err, os.ErrNotExist) {
					t.Fatal("committed private input was retained", err)
				} else if scenario != "committed" && err != nil {
					t.Fatal("unconfirmed private input was removed", err)
				}
			}
			if data, err := os.ReadFile(canonical); err != nil || string(data) != "canonical" {
				t.Fatal("input cleanup changed canonical data", err)
			}
			after, err := s.read(ctx)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("input cleanup changed quota or publication state", err)
			}
		})
	}
}
