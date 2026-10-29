// Package browseraccess applies the desktop-browser policy at the HTTP boundary.
package browseraccess

import (
	"net/http"
	"strings"

	"github.com/Activer007/Gokapi/internal/i18n"
)

// DesktopOnly rejects common phone/tablet browser identifiers before any handler
// runs. User-Agent is caller-controlled; this is not an authentication boundary.
func DesktopOnly(next http.Handler, defaultLanguage string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := strings.ToLower(r.UserAgent())
		for _, marker := range []string{"android", "iphone", "ipad", "ipod", "windows phone", "blackberry", "opera mini", "mobile", "silk/"} {
			if strings.Contains(agent, marker) {
				w.Header().Set("Cache-Control", "no-store")
				locale := i18n.Select(r, defaultLanguage)
				http.Error(w, locale.T("desktop.required"), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
