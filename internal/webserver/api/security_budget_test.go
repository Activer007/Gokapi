//go:build test

package api

import (
	"net/http/httptest"
	"testing"
)

func TestSEC014APIBudgetMatrix(t *testing.T) {
	transfer := map[string]bool{"/files/download/": true, "/files/downloadzip": true, "/chunk/add": true, "/chunk/complete": true, "/files/add": true, "/uploadrequest/chunk/add": true, "/uploadrequest/chunk/complete": true}
	for _, route := range routes {
		path := route.Url
		if route.HasWildcard {
			path += "synthetic-id"
		}
		if got := UsesTransferBudget(httptest.NewRequest("POST", "/api"+path, nil)); got != transfer[route.Url] {
			t.Errorf("wrong budget for %s", route.Url)
		}
	}
	for _, path := range []string{"/api/files/add-extra", "/api/chunk/add/extra", "/api/uploadrequest/chunk/add/extra", "/api/missing"} {
		if UsesTransferBudget(httptest.NewRequest("POST", path, nil)) {
			t.Errorf("invalid route inherited transfer budget: %s", path)
		}
	}
}
