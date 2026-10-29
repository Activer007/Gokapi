package uploadreceive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const spoolDirectory = ".gokapi-receive-v1"
const markerName = ".owner"
const markerContents = "Gokapi multipart receiver spool v1\n"
const maxSpoolEntries = 4096

var payloadName = regexp.MustCompile(`^receive-[0-9a-f]{32}$`)

// Prepare authenticates and cleans this pool's stale spool before serving.
// The old process must have exited; a live pool's leases must never be reset.
func (p *Pool) Prepare(ctx context.Context, dataDir string) error {
	if p == nil || ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.active != 0 || p.reserved != 0 || len(p.leases) != 0 || len(p.orphans) != 0 {
		return ErrBusy
	}
	return spoolRecoveryError(ctx, p.prepareSpoolContext(ctx, dataDir))
}

// VerifyRecovery is a read-only preflight: it neither creates a directory nor
// removes payloads or installs pool state. A missing spool can be prepared later.
func (p *Pool) VerifyRecovery(ctx context.Context, dataDir string) error {
	if p == nil || ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	abs, root, err := spoolPaths(dataDir)
	if err != nil {
		return spoolRecoveryError(ctx, err)
	}
	if p.dataDir != "" && p.dataDir != abs {
		return ErrInvalid
	}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) && !p.initialized {
		return ctx.Err()
	}
	_, err = spoolNames(ctx, root, false, nil)
	return spoolRecoveryError(ctx, err)
}

// Startup diagnostics must not contain filesystem paths from wrapped OS errors.
func spoolRecoveryError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrCapacity) {
		return ErrCapacity
	}
	return ErrInvalid
}

// Called under Pool.mu. Validate all entries before deleting any stale file.
func (p *Pool) prepareSpool(dataDir string) error {
	return p.prepareSpoolContext(context.Background(), dataDir)
}

func spoolPaths(dataDir string) (string, string, error) {
	if dataDir == "" {
		return "", "", ErrInvalid
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", "", errors.Join(ErrInvalid, err)
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.Join(ErrInvalid, err)
	}
	return abs, filepath.Join(abs, spoolDirectory), nil
}

func (p *Pool) prepareSpoolContext(ctx context.Context, dataDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, root, err := spoolPaths(dataDir)
	if err != nil {
		return err
	}
	if p.dataDir != "" && p.dataDir != abs {
		return ErrInvalid
	}
	p.dataDir, p.root = abs, root
	info, err := os.Lstat(p.root)
	if errors.Is(err, os.ErrNotExist) {
		if p.initialized {
			return ErrInvalid
		}
		if err := os.Mkdir(p.root, 0700); err != nil {
			return errors.Join(ErrInvalid, err)
		}
		marker, err := os.OpenFile(filepath.Join(p.root, markerName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.Join(ErrInvalid, err)
		}
		_, writeErr := io.WriteString(marker, markerContents)
		closeErr := marker.Close()
		if writeErr != nil || closeErr != nil {
			return errors.Join(ErrInvalid, writeErr, closeErr)
		}
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(ErrInvalid, err)
	}
	names, err := spoolNames(ctx, p.root, p.initialized, p.leases)
	if err != nil {
		return err
	}
	if !p.initialized {
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := p.remove(filepath.Join(p.root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.Join(ErrCapacity, err)
			}
		}
		p.initialized = true
	}
	return ctx.Err()
}

func spoolNames(ctx context.Context, root string, initialized bool, leases map[string]*lease) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSpoolRoot(root); err != nil {
		return nil, err
	}
	directory, err := os.Open(root)
	if err != nil {
		return nil, errors.Join(ErrInvalid, err)
	}
	entries, readErr := directory.ReadDir(maxSpoolEntries + 1)
	closeErr := directory.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
		return nil, errors.Join(ErrInvalid, readErr, closeErr)
	}
	if len(entries) > maxSpoolEntries {
		return nil, ErrInvalid
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Name() == markerName {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || !payloadName.MatchString(entry.Name()) {
			return nil, errors.Join(ErrInvalid, err)
		}
		if initialized && leases[entry.Name()] == nil {
			return nil, ErrInvalid
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

func (p *Pool) validateRoot() error {
	return validateSpoolRoot(p.root)
}

func validateSpoolRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(ErrInvalid, err)
	}
	path := filepath.Join(root, markerName)
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(markerContents)) {
		return errors.Join(ErrInvalid, err)
	}
	marker, err := os.Open(path)
	if err != nil {
		return errors.Join(ErrInvalid, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(marker, int64(len(markerContents))+1))
	closeErr := marker.Close()
	if readErr != nil || closeErr != nil || string(data) != markerContents {
		return errors.Join(ErrInvalid, readErr, closeErr)
	}
	return nil
}

func (p *Pool) createPayload(l *lease) (*os.File, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.validateRoot(); err != nil {
		return nil, err
	}
	for range 4 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, errors.Join(ErrInvalid, err)
		}
		name := "receive-" + hex.EncodeToString(random[:])
		file, err := os.OpenFile(filepath.Join(p.root, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, errors.Join(ErrInvalid, err)
		}
		l.name = name
		p.leases[name] = l
		return file, nil
	}
	return nil, ErrInvalid
}

func (p *Pool) finish(l *lease, closeUncertain bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.released {
		return nil
	}
	if l.active {
		p.active--
		l.active = false
	}
	l.closeUncertain = l.closeUncertain || closeUncertain
	if l.closeUncertain {
		// The OS may still hold disk blocks through the descriptor. Do not
		// unlink or refund them in this process, even if a retry would report
		// an already-closed file. Startup cleanup runs after the old process
		// has exited and can safely reclaim the authenticated regular file.
		if l.name != "" {
			p.orphans[l.name] = l
		}
		return ErrCapacity
	}
	if l.name != "" {
		if err := p.validateRoot(); err != nil {
			p.orphans[l.name] = l
			return err
		}
		if err := p.remove(filepath.Join(p.root, l.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			p.orphans[l.name] = l
			return err
		}
		delete(p.leases, l.name)
		delete(p.orphans, l.name)
	}
	p.reserved -= l.charge
	l.released = true
	return nil
}

func (p *Pool) retryCleanup() error {
	if len(p.orphans) > 64 {
		return ErrCapacity
	}
	var failure error
	for name, l := range p.orphans {
		if l.closeUncertain {
			failure = errors.Join(failure, ErrCapacity)
			continue
		}
		if err := p.remove(filepath.Join(p.root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failure = errors.Join(failure, err)
			continue
		}
		p.reserved -= l.charge
		l.released = true
		delete(p.orphans, name)
		delete(p.leases, name)
	}
	return failure
}
