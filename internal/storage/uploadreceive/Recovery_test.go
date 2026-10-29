package uploadreceive

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSEC030RecoveryReceiverVerifyThenPrepare(t *testing.T) {
	dir := t.TempDir()
	p, _ := New(1, 16<<20)
	if err := p.VerifyRecovery(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, spoolDirectory)); !errors.Is(err, os.ErrNotExist) || p.initialized || p.dataDir != "" || p.root != "" {
		t.Fatal("read-only preflight created a spool or installed pool state")
	}
	root := makeSpool(t, dir)
	stale := filepath.Join(root, "receive-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(stale, []byte("synthetic stale payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyRecovery(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(stale); err != nil || string(data) != "synthetic stale payload" || p.initialized || p.dataDir != "" {
		t.Fatal("read-only preflight changed stale input or pool state", err)
	}
	p.remove = func(path string) error { return &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission} }
	if err := p.Prepare(context.Background(), dir); err != ErrCapacity || strings.Contains(err.Error(), dir) {
		t.Fatal("startup cleanup error was not a fixed capacity error")
	}
	if _, err := os.Stat(stale); err != nil || p.initialized {
		t.Fatal("failed startup cleanup lost input or marked cleanup complete", err)
	}
	p.remove = os.Remove
	if err := p.Prepare(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) || !p.initialized {
		t.Fatal("startup did not reclaim authenticated stale input", err)
	}
	if err := p.Prepare(context.Background(), dir); err != nil {
		t.Fatal("startup preparation was not idempotent", err)
	}
	u, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("fresh"), false), dir, 100, 0)
	if err != nil {
		t.Fatal("prepared pool could not receive new input", err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, p)
}

func TestSEC030RecoveryReceiverForeignPreflight(t *testing.T) {
	dir := t.TempDir()
	root := makeSpool(t, dir)
	owned := filepath.Join(root, "receive-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	foreign := filepath.Join(root, "foreign-canary")
	for _, path := range []string{owned, foreign} {
		if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := New(1, 16<<20)
	if err := p.VerifyRecovery(context.Background(), dir); err != ErrInvalid || p.initialized || p.dataDir != "" {
		t.Fatal("foreign read-only preflight accepted input or installed state")
	}
	if err := p.Prepare(context.Background(), dir); err != ErrInvalid {
		t.Fatal("foreign spool was accepted for cleanup")
	}
	for _, path := range []string{owned, foreign} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "preserve" {
			t.Fatal("foreign entry rejection partially cleaned the spool", err)
		}
	}
}

func TestSEC030RecoveryReceiverCancellationAndRuntimeGuard(t *testing.T) {
	dir := t.TempDir()
	p, _ := New(1, 16<<20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Prepare(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preparation was accepted", err)
	}
	if err := p.VerifyRecovery(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled verification was accepted", err)
	}
	if p.Prepare(nil, dir) != ErrInvalid || p.VerifyRecovery(nil, dir) != ErrInvalid || p.initialized || p.dataDir != "" {
		t.Fatal("nil/cancelled context changed pool state")
	}
	if _, err := os.Lstat(filepath.Join(dir, spoolDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled preparation created a spool", err)
	}
	u, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("live"), false), dir, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	charge, path := p.reserved, u.File.Name()
	if p.Prepare(context.Background(), dir) != ErrBusy || p.active != 1 || p.reserved != charge {
		t.Fatal("startup preparation reset a live pool")
	}
	p.remove = func(string) error { return os.ErrPermission }
	if err := u.Close(); !errors.Is(err, os.ErrPermission) {
		t.Fatal("synthetic retained cleanup did not fail", err)
	}
	if p.Prepare(context.Background(), dir) != ErrBusy || p.active != 0 || p.reserved != charge || len(p.orphans) != 1 {
		t.Fatal("startup preparation bypassed a retained runtime lease")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "live" {
		t.Fatal("runtime guard removed retained input", err)
	}
	p.remove = os.Remove
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(context.Background(), dir); err != nil {
		t.Fatal("fully drained pool remained unavailable", err)
	}
	assertReleased(t, p)
}
