package chunksession

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

const (
	directoryName             = ".gokapi-chunks-v1"
	markerName                = ".owner"
	markerContents            = "Gokapi durable chunk sessions v1\n"
	maxRanges                 = 4096
	maxManifestBytes          = 1 << 20
	stagingOverhead     int64 = 1 << 20
	maxDirectoryEntries       = uploadquota.MaxRecords*3 + 1
)

var ownedName = regexp.MustCompile(`^(payload-[0-9a-f]{64}|manifest-[0-9a-f]{64}\.json|progress-[0-9a-f]{64}-[0-9a-f]{32})$`)

// ErrSpace preserves temporary capacity identity for the fixed public response.
var ErrSpace = errors.Join(uploadquota.ErrStaging, errors.New("not enough space on server for storing this file"))

type partReceipt struct {
	Offset int64
	Length int64
	SHA256 string
}
type manifest struct {
	Version   int
	ID        string
	UserID    int
	RequestID string
	Bytes     int64
	Ranges    []partReceipt
}

func (m manifest) complete() bool {
	if len(m.Ranges) == 0 {
		return false
	}
	end := int64(0)
	for _, r := range m.Ranges {
		if r.Offset != end {
			return false
		}
		end += r.Length
	}
	return end == m.Bytes
}

func key(id string) string                     { sum := sha256.Sum256([]byte(id)); return hex.EncodeToString(sum[:]) }
func (s *Store) path(name string) string       { return filepath.Join(s.root, directoryName, name) }
func (s *Store) payloadPath(id string) string  { return s.path("payload-" + key(id)) }
func (s *Store) manifestPath(id string) string { return s.path("manifest-" + key(id) + ".json") }

// Initialization validates the entire bounded namespace before any deletion or
// payload allocation. Orphans without a quota record require offline recovery,
// rather than silently entering an uncharged runtime namespace.
func (s *Store) prepareRoot(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return s.validateRoot()
	}
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	s.root = abs
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	info, err = os.Lstat(s.path(""))
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(s.path(""), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(s.path(markerName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(file, markerContents)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	entries, err := s.verifyEntriesLocked(ctx)
	if err != nil {
		return err
	}
	// These are unpublished progress replacements, never blob publication
	// targets. Initialization precedes any worker in this process, so a stale
	// writer cannot still own their descriptors. No quota is refunded here.
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "progress-") {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := os.Remove(s.path(entry.Name())); err != nil {
				return err
			}
		}
	}
	s.initialized = true
	return nil
}

// Called with Store.mu held. Verification never creates or removes files and
// always inspects the complete bounded namespace, even after initialization.
func (s *Store) verifyEntriesLocked(ctx context.Context) ([]os.DirEntry, error) {
	if err := s.validateRoot(); err != nil {
		return nil, err
	}
	entries, err := s.entries()
	if err != nil {
		return nil, err
	}
	ledger, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(ledger.Records))
	for id, r := range ledger.Records {
		if strings.HasPrefix(id, "chunk.") && r.BytesKnown && !r.StagingReleased {
			owned[key(id)] = true
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if name == markerName {
			continue
		}
		if !ownedName.MatchString(name) {
			return nil, uploadquota.ErrInvalid
		}
		info, err := os.Lstat(s.path(name))
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.Join(uploadquota.ErrInvalid, err)
		}
		var digest string
		switch {
		case strings.HasPrefix(name, "payload-"):
			digest = name[8:72]
		case strings.HasPrefix(name, "manifest-"):
			digest = name[9:73]
		default:
			digest = name[9:73]
		}
		if !owned[digest] {
			return nil, uploadquota.ErrInvalid
		}
	}
	return entries, nil
}

func (s *Store) validateRoot() error {
	info, err := os.Lstat(s.path(""))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	info, err = os.Lstat(s.path(markerName))
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(markerContents)) {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	file, err := os.Open(s.path(markerName))
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(len(markerContents))+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(data) != markerContents {
		return errors.Join(uploadquota.ErrInvalid, readErr, closeErr)
	}
	return nil
}

func (s *Store) entries() ([]os.DirEntry, error) {
	file, err := os.Open(s.path(""))
	if err != nil {
		return nil, err
	}
	entries, readErr := file.ReadDir(maxDirectoryEntries + 1)
	closeErr := file.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if len(entries) > maxDirectoryEntries {
		return nil, uploadquota.ErrFull
	}
	return entries, nil
}

func (s *Store) loadProgress(id string, r uploadquota.Reservation, create bool) (manifest, error) {
	if err := s.validateRoot(); err != nil {
		return manifest{}, err
	}
	path := s.manifestPath(id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if _, err := os.Lstat(s.payloadPath(id)); !errors.Is(err, os.ErrNotExist) {
			return manifest{}, errors.Join(uploadquota.ErrInvalid, err)
		}
		m := manifest{Version: 1, ID: id, UserID: r.UserID, RequestID: r.RequestID, Bytes: r.Bytes, Ranges: []partReceipt{}}
		if err := s.saveProgress(id, m); err != nil {
			return manifest{}, err
		}
		return m, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxManifestBytes {
		return manifest{}, errors.Join(uploadquota.ErrInvalid, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return manifest{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	closeErr := s.closeOwned(id, file)
	if readErr != nil || closeErr != nil {
		return manifest{}, errors.Join(readErr, closeErr)
	}
	m, err := decodeManifest(data)
	if err != nil || m.ID != id || m.UserID != r.UserID || m.RequestID != r.RequestID || m.Bytes != r.Bytes {
		return manifest{}, errors.Join(uploadquota.ErrInvalid, err)
	}
	return m, nil
}

func decodeManifest(data []byte) (manifest, error) {
	if len(data) == 0 || len(data) > maxManifestBytes {
		return manifest{}, uploadquota.ErrInvalid
	}
	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if err := uniqueJSON(check, 0); err != nil {
		return manifest{}, uploadquota.ErrInvalid
	}
	if _, err := check.Token(); err != io.EOF {
		return manifest{}, uploadquota.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var m manifest
	if err := d.Decode(&m); err != nil {
		return manifest{}, uploadquota.ErrInvalid
	}
	if m.Version != 1 || m.UserID <= 0 || !strings.HasPrefix(m.ID, "chunk.") || len(m.ID) > uploadquota.MaxIDLength || !boundedID.MatchString(m.ID) || len(m.RequestID) > uploadquota.MaxIDLength || (m.RequestID != "" && !boundedID.MatchString(m.RequestID)) || m.Bytes < 0 || m.Ranges == nil || len(m.Ranges) > maxRanges {
		return manifest{}, uploadquota.ErrInvalid
	}
	end := int64(0)
	for i, r := range m.Ranges {
		if r.Offset < 0 || r.Offset < end || r.Length < 0 || r.Offset > m.Bytes || r.Length > m.Bytes-r.Offset || len(r.SHA256) != 64 {
			return manifest{}, uploadquota.ErrInvalid
		}
		if _, err := hex.DecodeString(r.SHA256); err != nil || strings.ToLower(r.SHA256) != r.SHA256 {
			return manifest{}, uploadquota.ErrInvalid
		}
		if r.Length == 0 && (m.Bytes != 0 || i != 0 || len(m.Ranges) != 1) {
			return manifest{}, uploadquota.ErrInvalid
		}
		end = r.Offset + r.Length
	}
	return m, nil
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 8 {
		return uploadquota.ErrInvalid
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return uploadquota.ErrInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return uploadquota.ErrInvalid
			}
			seen[name] = true
		}
		if err := uniqueJSON(d, depth+1); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil {
		return err
	}
	if delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return uploadquota.ErrInvalid
	}
	return nil
}

func (s *Store) saveProgress(id string, m manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := decodeManifest(data); err != nil {
		return err
	}
	if err := s.validateRoot(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	path := s.path("progress-" + key(id) + "-" + hex.EncodeToString(random[:]))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := s.closeOwned(id, file)
	if closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if writeErr == nil {
		writeErr = os.Rename(path, s.manifestPath(id))
	}
	if writeErr != nil {
		removeErr := os.Remove(path)
		if removeErr != nil {
			s.mu.Lock()
			s.uncertain[id] = removeErr
			s.mu.Unlock()
		}
		return errors.Join(writeErr, removeErr)
	}
	return nil
}

func (s *Store) openPayload(id string, size int64, emptyProgress bool) (*os.File, error) {
	if err := s.validateRoot(); err != nil {
		return nil, err
	}
	path := s.payloadPath(id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// Only an empty progress record permits creating a new payload. A
		// missing file with confirmed ranges is corruption, not zero-filled data.
		if !emptyProgress {
			return nil, uploadquota.ErrInvalid
		}
		if err := s.checkSpace(size); err != nil {
			return nil, err
		}
		created, createErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return nil, createErr
		}
		allocationErr := created.Truncate(size)
		if allocationErr == nil {
			allocationErr = created.Sync()
		}
		if err := errors.Join(allocationErr, s.closeOwned(id, created)); err != nil {
			return nil, err
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.Mode().IsRegular() || (!emptyProgress && info.Size() != size) {
		return nil, errors.Join(uploadquota.ErrInvalid, err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	actual, statErr := file.Stat()
	if statErr == nil && actual.Mode().IsRegular() && emptyProgress && actual.Size() != size {
		statErr = s.checkSpace(size)
		if statErr == nil {
			statErr = file.Truncate(size)
		}
		if statErr == nil {
			statErr = file.Sync()
		}
		actual, err = file.Stat()
		statErr = errors.Join(statErr, err)
	}
	if statErr != nil || !actual.Mode().IsRegular() || actual.Size() != size {
		return nil, errors.Join(uploadquota.ErrInvalid, statErr, s.closeOwned(id, file))
	}
	return file, nil
}

func (s *Store) checkSpace(size int64) error {
	floor, err := s.freeFloor()
	if err != nil {
		return err
	}
	if size < 0 || floor < 0 || size > math.MaxInt64-floor {
		return uploadquota.ErrInvalid
	}
	free, err := s.freeSpace(s.root)
	if err != nil {
		return err
	}
	if free < uint64(size+floor) {
		return ErrSpace
	}
	return nil
}

func (s *Store) closeOwned(id string, file *os.File) error {
	err := s.closeFile(file)
	if err != nil {
		s.mu.Lock()
		s.uncertain[id] = err
		s.mu.Unlock()
	}
	return err
}

func (s *Store) cleanup(id string) error {
	s.mu.Lock()
	uncertain := s.uncertain[id]
	s.mu.Unlock()
	if uncertain != nil {
		return errors.Join(uploadquota.ErrInProgress, uncertain)
	}
	if err := s.validateRoot(); err != nil {
		return err
	}
	entries, err := s.entries()
	if err != nil {
		return err
	}
	prefix := "progress-" + key(id) + "-"
	paths := []string{s.payloadPath(id), s.manifestPath(id)}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			if !ownedName.MatchString(entry.Name()) {
				return uploadquota.ErrInvalid
			}
			paths = append(paths, s.path(entry.Name()))
		}
	}
	// Validate every candidate before removing the first one; never recurse.
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return errors.Join(uploadquota.ErrInvalid, err)
		}
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func copyExact(ctx context.Context, output io.Writer, input io.Reader, n int64) (string, error) {
	hash := sha256.New()
	written, err := io.CopyBuffer(io.MultiWriter(output, hash), contextReader{ctx: ctx, input: io.LimitReader(input, n)}, make([]byte, 32<<10))
	if err != nil {
		return "", err
	}
	if written != n {
		return "", uploadquota.ErrInvalid
	}
	// Probe without writing beyond the declared range, which may be directly
	// adjacent to bytes already confirmed by another successful request.
	var extra [1]byte
	count, err := io.ReadFull(contextReader{ctx: ctx, input: input}, extra[:])
	if count != 0 || !errors.Is(err, io.EOF) {
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return "", uploadquota.ErrInvalid
	}
	return hex.EncodeToString(hash.Sum(nil)), ctx.Err()
}

type contextReader struct {
	ctx   context.Context
	input io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}
