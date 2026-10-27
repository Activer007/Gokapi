//go:build test && !integration

package webserver

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"
)

// Real application route: complete headers, then a stalled management body.
// The old two-hour server read limit must not be the login form's budget.
func TestSEC014ManagementSlowBody(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:53843", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(35 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprint(conn, "POST /login HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 4096\r\n\r\nusername=synthetic&password=")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("management body still occupies the long transfer read budget")
	}
	if time.Since(started) < 29*time.Second {
		t.Fatal("fixture closed before the expected management budget")
	}
	if err == nil {
		resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == "session_token" && c.Value != "" {
				t.Fatal("incomplete login was authorized")
			}
		}
	}
}

func TestSEC014RouteBudgetMatrix(t *testing.T) {
	for _, pattern := range []string{"/auth/token", "/admin", "/apiKeys", "/changePassword", "/d", "/d/{id}/{filename}", "/e2eSetup", "/error", "/filerequests", "/forgotpw", "/index", "/login", "/logs", "/logout", "/publicUpload", "/users", "/oauth-login", "/oauth-callback", "/future-management"} {
		if usesTransferBudget(pattern, httptest.NewRequest("GET", pattern, nil)) {
			t.Errorf("management route has transfer budget: %s", pattern)
		}
	}
	for _, pattern := range []string{"/", "/downloadFile", "/downloadPresigned", "/dh/{id}/{filename}", "/h/", "/hotlink/", "/uploadChunk", "/uploadStatus", "/main.wasm", "/e2e.wasm", "/custom/", "/custom/admin.v1.js"} {
		if !usesTransferBudget(pattern, httptest.NewRequest("GET", pattern, nil)) {
			t.Errorf("transfer route has short budget: %s", pattern)
		}
	}
}

func TestSEC014BudgetDoesNotLeakAcrossKeepAlive(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("missing management context")
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/dh/{id}/{filename}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); ok {
			t.Error("short context reached file transfer")
		}
		if r.PathValue("id") != "synthetic" {
			t.Error("mux wildcard binding lost")
		}
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "synthetic content")
	})
	server := httptest.NewServer(withManagementBudget(mux, 50*time.Millisecond))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	resp, err := client.Get(server.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	reused := false
	req, _ := http.NewRequest("GET", server.URL+"/dh/synthetic/file", nil)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || string(data) != "synthetic content" || !reused {
		t.Fatalf("transfer/keep-alive failed: reused=%v err=%v", reused, err)
	}
}
