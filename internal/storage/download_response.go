package storage

import (
	"io"
	"net/http"
	"strconv"
)

// ServeContent cannot return a copy error. Observe its writes without replacing
// its range/conditional handling, and retain the underlying ReaderFrom path.
// This wrapper is local to serving; Unwrap keeps ResponseController available.
type downloadResponse struct {
	http.ResponseWriter
	status  int
	written int64
	err     error
}

func (w *downloadResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *downloadResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *downloadResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(body)
	w.written += int64(n)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

func (w *downloadResponse) ReadFrom(reader io.Reader) (int64, error) {
	if direct, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		n, err := direct.ReadFrom(reader)
		w.written += n
		if err != nil && w.err == nil {
			w.err = err
		}
		return n, err
	}
	// Hide ReadFrom from io.Copy so its fallback uses the observed Write.
	n, err := io.Copy(struct{ io.Writer }{w}, reader)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

func (w *downloadResponse) interrupted(r *http.Request) bool {
	if w.err != nil || r.Context().Err() != nil {
		return true
	}
	status := w.statusCode()
	if r.Method == http.MethodHead || (status != http.StatusOK && status != http.StatusPartialContent) {
		return false
	}
	length, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
	return err == nil && length >= 0 && w.written != length
}

func (w *downloadResponse) completed() bool {
	switch w.statusCode() {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified,
		http.StatusPreconditionFailed, http.StatusRequestedRangeNotSatisfiable:
		// A complete conditional/range rejection has no active file transfer.
		// Release its guard as before; this does not claim file delivery.
		return true
	}
	return false
}

func (w *downloadResponse) statusCode() int {
	if w.status == 0 {
		return http.StatusOK // net/http's implicit status for an empty response.
	}
	return w.status
}

type downloadCounter struct {
	io.Writer
	written int64
}

func (w *downloadCounter) Write(body []byte) (int, error) {
	n, err := w.Writer.Write(body)
	w.written += int64(n)
	return n, err
}
