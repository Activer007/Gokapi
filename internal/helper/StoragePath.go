package helper

import (
	"path/filepath"
	"strings"
)

// IsSafeLocalBlobName accepts a single canonical storage component, including
// legacy/E2E names with uppercase letters. It is not a path normalizer and must
// never be applied to S3 object keys. Ambiguous names retain their metadata for
// manual review instead of authorizing filesystem deletion.
func IsSafeLocalBlobName(name string) bool {
	if name == "" || len(name) > 128 || name == "." || name == ".." ||
		!filepath.IsLocal(name) || filepath.Base(name) != name || strings.HasSuffix(name, ".") {
		return false
	}
	for _, c := range []byte(name) {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '-' && c != '_' {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}
