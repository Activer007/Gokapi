// Package uploadreceive bounds multipart reception and its owned disk spool.
// It does not account for completed files or replace durable upload quotas.
package uploadreceive

import (
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/Activer007/Gokapi/internal/helper"
)

var (
	ErrBusy     = errors.New("upload receiver is busy")
	ErrCapacity = errors.New("upload receiver capacity exceeded")
	ErrSpace    = errors.New("insufficient upload receiver disk space")
	ErrTooLarge = errors.New("upload request is too large")
	ErrInvalid  = errors.New("invalid upload request or spool")
)

type Pool struct {
	mu                              sync.Mutex
	slots, budget, active, reserved int64
	free                            func(string) (uint64, error)
	remove                          func(string) error
	closeFile                       func(*os.File) error
	dataDir, root                   string
	initialized                     bool
	leases                          map[string]*lease
	orphans                         map[string]*lease
}

type Upload struct {
	File       *os.File
	Header     *multipart.FileHeader
	Fields     url.Values
	mu         sync.Mutex
	pool       *Pool
	lease      *lease
	fileClosed bool
	closeErr   error
}

type lease struct {
	name                 string
	charge, bodyReserved int64
	active, released     bool
	closeUncertain       bool
}

const (
	envelopeBytes int64 = 1 << 20
	chargeQuantum int64 = 1 << 20
	baseCharge    int64 = 64 << 10
	maxFields           = 32
	maxFieldBytes       = 64 << 10
)

func New(slots int64, budgetBytes int64) (*Pool, error) {
	if slots <= 0 || slots > 64 || budgetBytes <= 0 {
		return nil, ErrInvalid
	}
	return &Pool{slots: slots, budget: budgetBytes, free: helper.GetFreeSpace, remove: os.Remove, closeFile: (*os.File).Close,
		leases: make(map[string]*lease), orphans: make(map[string]*lease)}, nil
}

// Receive keeps one payload on disk and accepts both file-first and fields-first
// multipart clients. The caller owns the returned Upload and must Close it after
// storage processing. The request body remains owned by the HTTP caller.
func (p *Pool) Receive(w http.ResponseWriter, r *http.Request, dataDir string, maxFileBytes int64, minFreeBytes int64) (upload *Upload, err error) {
	if p == nil || r == nil || r.Body == nil || dataDir == "" || maxFileBytes < 0 || maxFileBytes > math.MaxInt64-envelopeBytes || minFreeBytes < 0 || r.ContentLength < -1 {
		return nil, ErrInvalid
	}
	ctx := r.Context()
	maxBody := maxFileBytes + envelopeBytes
	if r.ContentLength > maxBody {
		return nil, ErrTooLarge
	}
	l, err := p.acquire(ctx, dataDir, r.ContentLength, minFreeBytes)
	if err != nil {
		return nil, err
	}
	current := &Upload{pool: p, lease: l, Fields: make(url.Values)}
	defer func() {
		if err != nil {
			err = errors.Join(err, current.Close())
		}
	}()
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		return nil, http.ErrNotMultipart // preserve the existing public API error
	}
	if params["boundary"] == "" || len(params["boundary"]) > 70 {
		return nil, ErrInvalid
	}
	reader := &budgetReader{ctx: ctx, reader: http.MaxBytesReader(w, r.Body, maxBody), pool: p, lease: l, maxBody: maxBody, minFree: minFreeBytes}
	multipartReader := multipart.NewReader(reader, params["boundary"])
	fieldBytes, fields := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, partErr := multipartReader.NextRawPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			return nil, receiveError(partErr)
		}
		name := part.FormName()
		if name == "" {
			return nil, ErrInvalid
		}
		_, disposition, parseErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if parseErr != nil {
			return nil, ErrInvalid
		}
		_, isFile := disposition["filename"]
		var partReader io.Reader = part
		if isFile {
			// Count file bytes on the wire before any transfer decoding. The
			// raw body budget and non-file envelope must not count encoding
			// expansion as MIME overhead.
			partReader = wireCountingReader{reader: part, count: &reader.payloadBytes}
		}
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
			// Match multipart.NextPart/ReadForm's existing public API semantics
			// while keeping raw file accounting outside the decoder.
			partReader = quotedprintable.NewReader(partReader)
			part.Header.Del("Content-Transfer-Encoding")
		}
		if isFile {
			if name != "file" || current.File != nil || part.FileName() == "" {
				return nil, ErrInvalid
			}
			file, err := p.createPayload(l)
			if err != nil {
				return nil, err
			}
			current.File = file
			size, err := copyPayload(ctx, file, partReader, maxFileBytes)
			if err != nil {
				return nil, receiveError(err)
			}
			current.Header = &multipart.FileHeader{Filename: part.FileName(), Header: part.Header, Size: size}
		} else {
			if name == "file" || current.Fields.Has(name) {
				return nil, ErrInvalid
			}
			fields++
			if fields > maxFields || len(name) > maxFieldBytes-fieldBytes {
				return nil, ErrTooLarge
			}
			remaining := maxFieldBytes - fieldBytes - len(name)
			value, err := io.ReadAll(io.LimitReader(contextReader{ctx, partReader}, int64(remaining)+1))
			if err != nil {
				return nil, receiveError(err)
			}
			if len(value) > remaining {
				return nil, ErrTooLarge
			}
			fieldBytes += len(name) + len(value)
			current.Fields.Set(name, string(value))
		}
	}
	if current.File == nil {
		return nil, ErrInvalid
	}
	// NextPart may finish before consuming a MIME epilogue. Count and bound the
	// rest of the actual body as well; never trust a declared Content-Length.
	if _, err := io.Copy(io.Discard, contextReader{ctx, reader}); err != nil {
		return nil, receiveError(err)
	}
	if reader.read-reader.payloadBytes > envelopeBytes {
		return nil, ErrTooLarge
	}
	if _, err := current.File.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return current, nil
}

func copyPayload(ctx context.Context, file *os.File, reader io.Reader, max int64) (int64, error) {
	buffer := make([]byte, 32<<10)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return size, err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if int64(n) > max-size {
				return size, ErrTooLarge
			}
			if err := ctx.Err(); err != nil {
				return size, err
			}
			written, err := file.Write(buffer[:n])
			if err != nil {
				return size, err
			}
			if written != n {
				return size, io.ErrShortWrite
			}
			size += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			return size, nil
		}
		if readErr != nil {
			return size, readErr
		}
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

type wireCountingReader struct {
	reader io.Reader
	count  *int64
}

func (r wireCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	*r.count += int64(n)
	return n, err
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type budgetReader struct {
	ctx                                  context.Context
	reader                               io.Reader
	pool                                 *Pool
	lease                                *lease
	read, payloadBytes, maxBody, minFree int64
}

func (r *budgetReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.read += int64(n) // MaxBytesReader bounds the counter below maxBody.
		// multipart and transfer decoding may prefetch before the raw file
		// counter advances. Bound that fixed-buffer lag; the final check has
		// no slack and permits at most envelopeBytes of non-file wire bytes.
		if r.read-r.payloadBytes > envelopeBytes+(36<<10) {
			return 0, ErrTooLarge
		}
		if growErr := r.pool.grow(r.ctx, r.lease, r.read, r.maxBody, r.minFree); growErr != nil {
			return 0, growErr
		}
	}
	return n, err
}

func reservationCharge(body int64) (int64, error) {
	if body < 0 || body > (math.MaxInt64-baseCharge)/4 {
		return 0, ErrCapacity
	}
	return body*4 + baseCharge, nil
}

func (p *Pool) acquire(ctx context.Context, dataDir string, length, minFree int64) (*lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.active >= p.slots {
		return nil, ErrBusy
	}
	if err := p.prepareSpool(dataDir); err != nil {
		return nil, err
	}
	if err := p.retryCleanup(); err != nil {
		return nil, errors.Join(ErrCapacity, err)
	}
	body := max(length, 0)
	charge, err := reservationCharge(body)
	if err != nil {
		return nil, err
	}
	if err := p.checkCapacity(charge, minFree); err != nil {
		return nil, err
	}
	l := &lease{charge: charge, bodyReserved: body, active: true}
	p.active++
	p.reserved += charge
	return l, nil
}

func (p *Pool) grow(ctx context.Context, l *lease, actual, maxBody, minFree int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.released {
		return ErrInvalid
	}
	if actual <= l.bodyReserved {
		return nil
	}
	target := actual
	if remainder := target % chargeQuantum; remainder != 0 {
		if target > math.MaxInt64-(chargeQuantum-remainder) {
			return ErrCapacity
		}
		target += chargeQuantum - remainder
	}
	target = min(target, maxBody)
	charge, err := reservationCharge(target)
	if err != nil {
		return err
	}
	if err := p.checkCapacity(charge-l.charge, minFree); err != nil {
		return err
	}
	p.reserved += charge - l.charge
	l.charge, l.bodyReserved = charge, target
	return nil
}

func (p *Pool) checkCapacity(extra, minFree int64) error {
	if extra < 0 || extra > p.budget-p.reserved {
		return ErrCapacity
	}
	free, err := p.free(p.dataDir)
	if err != nil {
		return errors.Join(ErrSpace, err)
	}
	if free < uint64(p.reserved+extra)+uint64(minFree) {
		return ErrSpace
	}
	return nil
}

// Close is safe to repeat. Failed deletion retains its charge and may be
// retried. An uncertain file close retains both the payload and its charge
// until process restart; it cannot be cleared by another close or admission.
// Both failure cases release the request slot.
func (u *Upload) Close() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.fileClosed {
		u.fileClosed = true
		if u.File != nil {
			u.closeErr = u.pool.closeFile(u.File)
			if errors.Is(u.closeErr, os.ErrClosed) {
				u.closeErr = nil
			}
		}
	}
	return errors.Join(u.closeErr, u.pool.finish(u.lease, u.closeErr != nil))
}

func receiveError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
		return errors.Join(ErrTooLarge, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBusy) || errors.Is(err, ErrCapacity) || errors.Is(err, ErrSpace) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrInvalid) {
		return err
	}
	return errors.Join(ErrInvalid, err)
}
