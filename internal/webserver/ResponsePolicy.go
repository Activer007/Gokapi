package webserver

import "net/http"

const pageReportOnlyPolicy = "base-uri 'self'; object-src 'none'; script-src 'self' 'wasm-unsafe-eval'"

// withResponsePolicy passes the original writer through so streaming handlers
// retain Flusher, Hijacker and ResponseController support.
func withResponsePolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
