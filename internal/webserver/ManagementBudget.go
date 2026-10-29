package webserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/Activer007/Gokapi/internal/webserver/api"
	"github.com/Activer007/Gokapi/internal/webserver/requestbudget"
)

const managementBudget = 30 * time.Second

func withManagementBudget(mux *http.ServeMux, budget time.Duration) http.Handler {
	management := requestbudget.Management(mux, budget)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if usesTransferBudget(pattern, r) {
			mux.ServeHTTP(w, r)
			return
		}
		// Go's mux must still dispatch the request to populate wildcard PathValues.
		management.ServeHTTP(w, r)
	})
}

func usesTransferBudget(pattern string, r *http.Request) bool {
	switch pattern {
	case "/", "/downloadFile", "/downloadPresigned", "/dh/{id}/{filename}",
		"/h/", "/hotlink/", "/uploadChunk", "/uploadStatus", "/main.wasm", "/e2e.wasm":
		return true
	case "/api/":
		return api.UsesTransferBudget(r)
	}
	return strings.HasPrefix(pattern, "/custom/")
}
