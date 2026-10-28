package uploadreceive

import (
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

// Called under Pool.mu. Validate all entries before deleting any stale file.
func (p *Pool) prepareSpool(dataDir string) error {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return errors.Join(ErrInvalid, err)
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(ErrInvalid, err)
	}
	if p.dataDir != "" && p.dataDir != abs {
		return ErrInvalid
	}
	p.dataDir, p.root = abs, filepath.Join(abs, spoolDirectory)
	info, err = os.Lstat(p.root)
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
	if err := p.validateRoot(); err != nil {
		return err
	}
	directory, err := os.Open(p.root)
	if err != nil {
		return errors.Join(ErrInvalid, err)
	}
	entries, readErr := directory.ReadDir(maxSpoolEntries + 1)
	closeErr := directory.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
		return errors.Join(ErrInvalid, readErr, closeErr)
	}
	if len(entries) > maxSpoolEntries {
		return ErrInvalid
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == markerName {
			continue
		}
		info, err := os.Lstat(filepath.Join(p.root, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || !payloadName.MatchString(entry.Name()) {
			return errors.Join(ErrInvalid, err)
		}
		if p.initialized && p.leases[entry.Name()] == nil {
			return ErrInvalid
		}
		names = append(names, entry.Name())
	}
	if !p.initialized {
		for _, name := range names {
			if err := p.remove(filepath.Join(p.root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.Join(ErrCapacity, err)
			}
		}
		p.initialized = true
	}
	return nil
}

func (p *Pool) validateRoot() error {
	info, err := os.Lstat(p.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(ErrInvalid, err)
	}
	path := filepath.Join(p.root, markerName)
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
