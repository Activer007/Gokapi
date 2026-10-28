package uploadreceive

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type admissionBody struct {
	reader  io.Reader
	entered chan<- struct{}
	release <-chan struct{}
	once    sync.Once
	reads   atomic.Int64
}

func (b *admissionBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	b.once.Do(func() {
		select {
		case b.entered <- struct{}{}:
		default:
		}
		<-b.release
	})
	return b.reader.Read(p)
}
func (b *admissionBody) Close() error { return nil }

func TestReceiveConcurrentAdmissionAndClose(t *testing.T) {
	for _, concurrent := range []int{10, 50} {
		t.Run(strconv.Itoa(concurrent), func(t *testing.T) {
			p, _ := New(4, 64<<20)
			dir := t.TempDir()
			entered := make(chan struct{}, 4)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			type outcome struct {
				u   *Upload
				err error
			}
			accepted := make(chan outcome, 4)
			for range 4 {
				r := receiveRequest(t, true, nil, []byte("bounded bytes"), true)
				r.Body = &admissionBody{reader: r.Body, entered: entered, release: release}
				go func(r *http.Request) {
					u, err := p.Receive(httptest.NewRecorder(), r, dir, 1024, 0)
					accepted <- outcome{u, err}
				}(r)
			}
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			for range 4 {
				select {
				case <-entered:
				case <-timer.C:
					t.Fatal("admitted readers did not enter")
				}
			}
			// All slots now hold real Receive calls paused at their first read.
			// Every additional call must fail before touching its body.
			type rejected struct {
				err   error
				reads int64
			}
			results := make(chan rejected, concurrent)
			for range concurrent {
				r := receiveRequest(t, true, nil, nil, true)
				body := &admissionBody{reader: r.Body, entered: entered, release: release}
				r.Body = body
				go func(r *http.Request, b *admissionBody) {
					_, err := p.Receive(httptest.NewRecorder(), r, dir, 1024, 0)
					results <- rejected{err, b.reads.Load()}
				}(r, body)
			}
			for range concurrent {
				select {
				case got := <-results:
					if !errors.Is(got.err, ErrBusy) || got.reads != 0 {
						t.Fatalf("occupied admission: %v reads=%d", got.err, got.reads)
					}
				case <-timer.C:
					t.Fatal("occupied admission queued instead of rejecting")
				}
			}
			unblock()
			var closeWG sync.WaitGroup
			closeErrors := make(chan error, 8)
			for range 4 {
				select {
				case got := <-accepted:
					if got.err != nil {
						t.Fatal(got.err)
					}
					for range 2 {
						closeWG.Add(1)
						go func(u *Upload) { defer closeWG.Done(); closeErrors <- u.Close() }(got.u)
					}
				case <-timer.C:
					t.Fatal("admitted receive did not finish")
				}
			}
			closeWG.Wait()
			close(closeErrors)
			for err := range closeErrors {
				if err != nil {
					t.Fatal(err)
				}
			}
			assertReleased(t, p)
		})
	}
}
