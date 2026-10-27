//go:build test

package securityfixture

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

// NewIDP creates an immutable identity per server. Start two servers to model
// distinct issuers with the same email. Keys are ephemeral, never serialized.
// This is a protocol fixture, not a browser or a full authorization server.
func NewIDP(t *testing.T, identity Identity) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	writeJSON := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/auth", "token_endpoint": server.URL + "/token", "userinfo_endpoint": server.URL + "/userinfo", "jwks_uri": server.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{map[string]string{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "fixture", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-access" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"sub": identity.Subject, "email": identity.Email, "email_verified": identity.EmailVerified})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != "synthetic-code" {
			http.Error(w, "invalid code", http.StatusBadRequest)
			return
		}
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture"}`))
		body, _ := json.Marshal(map[string]any{"iss": server.URL, "sub": identity.Subject, "aud": "fixture-client", "email": identity.Email, "email_verified": identity.EmailVerified, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()})
		unsigned := header + "." + base64.RawURLEncoding.EncodeToString(body)
		digest := sha256.Sum256([]byte(unsigned))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			http.Error(w, "signing failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"access_token": "synthetic-access", "token_type": "Bearer", "expires_in": 60, "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
