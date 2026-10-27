//go:build test

package requestbudget

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSEC014BudgetContext(t *testing.T) {
	reached := false
	Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("missing management deadline")
		}
		<-r.Context().Done()
		if !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			t.Error("deadline not propagated")
		}
	}), 20*time.Millisecond).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin", nil))
	if !reached {
		t.Fatal("handler not reached")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	Management(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("canceled handler ran") }), time.Second).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin", nil).WithContext(canceled))
}

func TestSEC014BudgetSlowRead(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		name := "http1"
		if h2 {
			name = "http2"
		}
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			started := make(chan int, 1)
			server := httptest.NewUnstartedServer(Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				started <- r.ProtoMajor
				_, err := io.ReadAll(r.Body)
				done <- err
			}), 100*time.Millisecond))
			server.EnableHTTP2 = h2
			if h2 {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			req, _ := http.NewRequest("POST", server.URL, reader)
			req.ContentLength = 4096
			client := server.Client()
			client.Timeout = 2 * time.Second
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				resp, _ := client.Do(req)
				if resp != nil {
					resp.Body.Close()
				}
			}()
			select {
			case major := <-started:
				if h2 && major != 2 {
					t.Fatal("HTTP/2 not negotiated")
				}
			case <-time.After(time.Second):
				t.Fatal("handler did not start")
			}
			select {
			case err := <-done:
				if err == nil {
					t.Error("incomplete management body was accepted")
				}
			case <-time.After(time.Second):
				t.Error("body read did not stop at deadline")
			}
			writer.Close()
			reader.Close()
			<-clientDone
		})
	}
}

func TestSEC014BudgetSlowWrite(t *testing.T) {
	done := make(chan error, 1)
	server := httptest.NewServer(Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		block := []byte(strings.Repeat("x", 65536))
		// The peer deliberately reads nothing. Reuse one buffer; no unbounded fixture allocation.
		for {
			if _, err := w.Write(block); err != nil {
				done <- err
				return
			}
		}
	}), 100*time.Millisecond))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = io.WriteString(conn, "GET /admin HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected write deadline error")
		}
	case <-time.After(2 * time.Second):
		conn.Close()
		t.Fatal("blocked response exceeded deadline")
	}
}
