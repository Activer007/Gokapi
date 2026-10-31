//go:build test

package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestQuotaSummaryLogicalUsageAndSubjectIsolation(t *testing.T) {
	owner := models.User{Id: 7101, Name: "quota-owner-" + rand.Text(), UserLevel: models.UserLevelUser}
	admin := models.User{Id: 7102, Name: "quota-admin-" + rand.Text(), UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}
	for _, user := range []models.User{owner, admin} {
		database.SaveUser(user, false)
		t.Cleanup(func() { database.DeleteUser(user.Id) })
	}
	ownerKey := generateNewKey(true, owner.Id, "quota-owner", "")
	adminKey := generateNewKey(true, admin.Id, "quota-admin", "")
	noView := generateNewKey(false, owner.Id, "quota-no-view", "")
	publicKey := generateNewKey(true, owner.Id, "public upload", "quota-request")
	for _, key := range []models.ApiKey{ownerKey, adminKey, noView, publicKey} {
		t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	}
	files := []models.File{
		{Id: "quota-" + rand.Text(), UserId: owner.Id, SHA1: "private-reused-blob", SizeBytes: 64, ExpireAt: 1},
		{Id: "quota-" + rand.Text(), UserId: owner.Id, SHA1: "private-reused-blob", SizeBytes: 64, PendingDeletion: 1},
		{Id: "quota-" + rand.Text(), UserId: admin.Id, SHA1: "another-private-blob", SizeBytes: 999},
	}
	for _, file := range files {
		database.SaveMetaData(file)
		t.Cleanup(func() { database.DeleteMetaData(file.Id) })
	}
	request := func(key, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/quota/summary", nil)
		r.Header.Set("apikey", key)
		r.Header.Set("userId", "7101") // An administrator still cannot select another subject.
		w := httptest.NewRecorder()
		Process(w, r)
		return w
	}
	for _, key := range []string{"", noView.Id, publicKey.Id} {
		if got := request(key, http.MethodGet); got.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized quota status=%d", got.Code)
		}
	}
	if got := request(ownerKey.Id, http.MethodPost); got.Code != http.StatusBadRequest {
		t.Fatal("quota write method accepted", got.Code)
	}
	for _, check := range []struct {
		key          string
		bytes, files int64
	}{{ownerKey.Id, 128, 2}, {adminKey.Id, 999, 1}} {
		w := request(check.key, http.MethodGet)
		var output quotaSummaryOutput
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &output) != nil || !output.Available || output.UsedBytes != check.bytes || output.UsedFiles != check.files {
			t.Fatalf("quota output=%s status=%d", w.Body.String(), w.Code)
		}
		env := configuration.GetEnvironment()
		if output.LimitBytes != env.UploadUserTotalMB*(1024*1024) || output.LimitFiles != env.UploadUserMaxFiles || output.AsOf < time.Now().Add(-time.Minute).Unix() {
			t.Fatal("quota omitted effective limits or completion time")
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "reserved") || strings.Contains(w.Body.String(), "instance") {
			t.Fatal("quota summary cached or exposed unrelated metadata")
		}
	}
	database.DeleteMetaData(files[1].Id)
	w := request(ownerKey.Id, http.MethodGet)
	var output quotaSummaryOutput
	if json.Unmarshal(w.Body.Bytes(), &output) != nil || output.UsedBytes != 64 || output.UsedFiles != 1 {
		t.Fatal("retired metadata remained charged", w.Body.String())
	}
}

func TestQuotaSummaryLimitsAndBoundedFailure(t *testing.T) {
	env := configuration.GetEnvironment()
	env.UploadUserTotalMB, env.UploadUserMaxFiles = 7, 3
	read := func(ctx context.Context, userID int) (uploadquota.Usage, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || userID != 7 {
			t.Fatal("quota read lacks a bounded, authenticated subject")
		}
		return uploadquota.Usage{Bytes: 123, Files: 2}, nil
	}
	before := time.Now().Unix()
	output, err := readQuotaSummary(context.Background(), 7, env, read)
	if err != nil || !output.Available || output.LimitBytes != 7*1024*1024 || output.LimitFiles != 3 || output.AsOf < before {
		t.Fatal("effective limits or timestamp", output, err)
	}
	for _, value := range []int64{0, -1, math.MaxInt64/(1024*1024) + 1} {
		bad := env
		bad.UploadUserTotalMB = value
		if output, err := readQuotaSummary(context.Background(), 7, bad, func(context.Context, int) (uploadquota.Usage, error) {
			t.Fatal("invalid limits triggered a provider read")
			return uploadquota.Usage{}, nil
		}); !errors.Is(err, errQuotaSummaryUnavailable) || output.Available {
			t.Fatal("invalid limits treated as unlimited", output, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readQuotaSummary(ctx, 7, env, read); !errors.Is(err, errQuotaSummaryUnavailable) {
		t.Fatal("canceled read was available", err)
	}
	if output, err := readQuotaSummary(context.Background(), 7, env, func(context.Context, int) (uploadquota.Usage, error) {
		return uploadquota.Usage{Bytes: 123, Files: 2}, errors.New("PRIVATE_LEDGER /srv/private other-user=99")
	}); !errors.Is(err, errQuotaSummaryUnavailable) || output.Available || output.UsedBytes != 0 {
		t.Fatal("provider failure leaked a partial result", output, err)
	}
}

func TestQuotaSummaryUnavailableResponseHasNoOptimisticNumbers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/quota/summary", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	apiQuotaSummary(w, &paramQuotaSummary{Request: r}, models.User{Id: 7}, models.ApiKey{})
	var output map[string]any
	if w.Code != http.StatusServiceUnavailable || json.Unmarshal(w.Body.Bytes(), &output) != nil || output["available"] != false || output["Result"] != "error" {
		t.Fatal("unavailable response", w.Code, w.Body.String())
	}
	for _, field := range []string{"usedBytes", "usedFiles", "limitBytes", "limitFiles", "asOf"} {
		if _, found := output[field]; found {
			t.Fatal("unavailable response contains quota numbers", field)
		}
	}
	if output["ErrorMessage"] != "Quota summary temporarily unavailable" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("unavailable response leaked diagnostics or allowed caching")
	}
}
