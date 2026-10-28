package uploadreceive

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func makeSpool(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, spoolDirectory)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, markerName), []byte(markerContents), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSpoolRestartCleanupAndRetry(t *testing.T) {
	dir := t.TempDir()
	root := makeSpool(t, dir)
	stale := filepath.Join(root, "receive-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(stale, []byte("old synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := New(1, 16<<20)
	p.remove = func(string) error { return os.ErrPermission }
	call := func() (*Upload, error) {
		return p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("fresh"), false), dir, 100, 0)
	}
	if _, err := call(); !errors.Is(err, ErrCapacity) {
		t.Fatal("startup cleanup failure did not block", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("startup failure lost owned file", err)
	}
	assertReleased(t, p)
	p.remove = os.Remove
	u, err := call()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("restart retained old payload", err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, p)
	// A new process-equivalent pool authenticates the same root and accepts it.
	p2, _ := New(1, 16<<20)
	u, err = p2.Receive(httptest.NewRecorder(), receiveRequest(t, false, nil, nil, true), dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, p2)
}

func TestSpoolRejectsUnownedEntriesBeforeDeletingAnything(t *testing.T) {
	for _, kind := range []string{"missing-marker", "wrong-marker", "unknown-file", "directory", "symlink", "marker-symlink", "root-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			root := makeSpool(t, dir)
			stale := filepath.Join(root, "receive-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			if err := os.WriteFile(stale, []byte("keep until all entries validated"), 0600); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "canary")
			if err := os.WriteFile(outside, []byte("protected"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-marker":
				if err := os.Remove(filepath.Join(root, markerName)); err != nil {
					t.Fatal(err)
				}
			case "wrong-marker":
				if err := os.WriteFile(filepath.Join(root, markerName), []byte("unowned"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-file":
				if err := os.WriteFile(filepath.Join(root, "multipart-unowned"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filepath.Join(root, "receive-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(outside, filepath.Join(root, "receive-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")); err != nil {
					t.Fatal(err)
				}
			case "marker-symlink":
				if err := os.Remove(filepath.Join(root, markerName)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, markerName)); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				renamed := root + "-saved"
				if err := os.Rename(root, renamed); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(renamed, root); err != nil {
					t.Fatal(err)
				}
			}
			p, _ := New(1, 16<<20)
			_, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), dir, 0, 0)
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("unowned spool accepted", err)
			}
			if _, err := os.Stat(stale); err != nil {
				t.Fatal("partial cleanup before validation", err)
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "protected" {
				t.Fatal("canary modified", err)
			}
			assertReleased(t, p)
		})
	}
}

func TestSpoolScanIsBounded(t *testing.T) {
	dir := t.TempDir()
	root := makeSpool(t, dir)
	for i := 0; i < maxSpoolEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("receive-%032x", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := New(1, 16<<20)
	_, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), dir, 0, 0)
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("oversize directory accepted", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != maxSpoolEntries+1 {
		t.Fatal("bounded scan deleted files", len(entries), err)
	}
	assertReleased(t, p)
}

func TestSpoolChangedWhileUploadLiveRetainsCharge(t *testing.T) {
	dir := t.TempDir()
	p, _ := New(1, 16<<20)
	u, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("owned"), false), dir, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, spoolDirectory, markerName)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing marker cleanup accepted", err)
	}
	if p.active != 0 || p.reserved == 0 {
		t.Fatal("unsafe cleanup released accounting")
	}
	if err := os.WriteFile(marker, []byte(markerContents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, p)
}

func TestSpoolUncertainCloseRequiresRestart(t *testing.T) {
	dir := t.TempDir()
	p, err := New(1, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("owned bytes"), false), dir, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := u.File.Name()
	charge := p.reserved
	closeFailure := syscall.EIO
	closeCalls := 0
	p.closeFile = func(file *os.File) error {
		closeCalls++
		if err := file.Close(); err != nil {
			return err
		}
		// Close the real descriptor for safe synthetic process-restart tests,
		// but report an uncertain result to the receiver.
		return closeFailure
	}
	for range 2 {
		if err := u.Close(); !errors.Is(err, closeFailure) {
			t.Fatalf("uncertain close = %v", err)
		}
		if p.active != 0 || p.reserved != charge || len(p.orphans) != 1 || !u.lease.closeUncertain {
			t.Fatal("uncertain close released accounting or retained its request slot")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("uncertain close unlinked its owned payload", err)
		}
	}
	if closeCalls != 1 {
		t.Fatal("uncertain close was retried and could mask the first error")
	}
	if _, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), dir, 100, 0); !errors.Is(err, ErrCapacity) {
		t.Fatal("new admission bypassed uncertain descriptor cleanup", err)
	}
	if p.reserved != charge {
		t.Fatal("admission cleanup refunded an uncertain close")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("admission cleanup unlinked an uncertain payload", err)
	}
	// A fresh pool models a restarted process, after the old OS descriptors
	// have gone. It must authenticate the root and remove the stale payload
	// before admitting new bytes. The old pool is never used again.
	restarted, err := New(1, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), dir, 100, 0)
	if err != nil {
		t.Fatal("restart did not reclaim owned stale payload", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("restart retained stale uncertain-close payload", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, restarted)
}
