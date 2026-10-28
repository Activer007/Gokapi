package environment

import (
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSEC030ReceivePolicyDefaultsAndBoundaries(t *testing.T) {
	for _, key := range []string{"GOKAPI_UPLOAD_BODY_SLOTS", "GOKAPI_UPLOAD_RECEIVE_MB"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	got := New()
	if got.UploadBodySlots != 4 || got.UploadReceiveMB != 12288 {
		t.Fatalf("receive defaults: slots=%d MiB=%d", got.UploadBodySlots, got.UploadReceiveMB)
	}
	for _, limits := range []Environment{
		{UploadBodySlots: 1, UploadReceiveMB: 1},
		{UploadBodySlots: 4, UploadReceiveMB: 12288},
		{UploadBodySlots: 64, UploadReceiveMB: math.MaxInt64 / (1024 * 1024)},
	} {
		if err := limits.ValidateReceiveLimits(); err != nil {
			t.Fatalf("valid receive limits rejected: %v", err)
		}
	}
	for _, field := range []string{"UploadBodySlots", "UploadReceiveMB"} {
		definition, ok := reflect.TypeOf(Environment{}).FieldByName(field)
		if !ok || definition.Tag.Get("onlyPositive") != "" || definition.Tag.Get("minValue") != "" {
			t.Fatalf("%s silently normalizes explicit invalid receive policy", field)
		}
	}
}

func TestSEC030ReceivePolicyRejectsInvalid(t *testing.T) {
	for name, limits := range map[string]Environment{
		"zero-slots":      {UploadBodySlots: 0, UploadReceiveMB: 12288},
		"negative-slots":  {UploadBodySlots: -1, UploadReceiveMB: 12288},
		"excessive-slots": {UploadBodySlots: 65, UploadReceiveMB: 12288},
		"zero-MiB":        {UploadBodySlots: 4, UploadReceiveMB: 0},
		"negative-MiB":    {UploadBodySlots: 4, UploadReceiveMB: -1},
		"bytes-overflow":  {UploadBodySlots: 4, UploadReceiveMB: math.MaxInt64/(1024*1024) + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := limits.ValidateReceiveLimits(); err == nil {
				t.Fatal("invalid receive capacities accepted")
			}
			copy := limits
			if err := enforceIntLimits(&copy); err == nil {
				t.Fatal("environment enforcement accepted invalid receive capacities")
			}
			if copy.UploadBodySlots != limits.UploadBodySlots || copy.UploadReceiveMB != limits.UploadReceiveMB {
				t.Fatal("invalid explicit capacity was silently rewritten")
			}
		})
	}
}

func receivePolicyOutput(t *testing.T, call func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous; _ = reader.Close(); _ = writer.Close() }()
	call()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = previous
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestSEC030ReceivePolicyNewFailsClosedWithoutValueEcho(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"zero-slots", "GOKAPI_UPLOAD_BODY_SLOTS", "0"},
		{"negative-slots", "GOKAPI_UPLOAD_BODY_SLOTS", "-1"},
		{"excessive-slots", "GOKAPI_UPLOAD_BODY_SLOTS", "65"},
		{"zero-MiB", "GOKAPI_UPLOAD_RECEIVE_MB", "0"},
		{"negative-MiB", "GOKAPI_UPLOAD_RECEIVE_MB", "-1"},
		{"conversion-overflow", "GOKAPI_UPLOAD_RECEIVE_MB", strconv.FormatInt(math.MaxInt64/(1024*1024)+1, 10)},
		{"numeric-overflow", "GOKAPI_UPLOAD_RECEIVE_MB", "9223372036854775808"},
		{"synthetic-sensitive-value", "GOKAPI_UPLOAD_BODY_SLOTS", "SEC030_SYNTHETIC_VALUE_MUST_NOT_APPEAR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOKAPI_UPLOAD_BODY_SLOTS", "4")
			t.Setenv("GOKAPI_UPLOAD_RECEIVE_MB", "12288")
			t.Setenv(tc.key, tc.value)
			originalExit := osExit
			defer func() { osExit = originalExit }()
			exits := 0
			osExit = func(code int) {
				if code != 1 {
					t.Errorf("unexpected exit code %d", code)
				}
				exits++
			}
			var got Environment
			output := receivePolicyOutput(t, func() { got = New() })
			if exits == 0 {
				t.Fatal("invalid receive environment did not fail startup")
			}
			if got.isSet || got.UploadBodySlots != 0 || got.UploadReceiveMB != 0 {
				t.Fatal("failed startup returned an apparently valid environment")
			}
			if tc.name == "synthetic-sensitive-value" || tc.name == "numeric-overflow" || tc.name == "conversion-overflow" {
				if strings.Contains(output, tc.value) {
					t.Fatal("configuration error echoed raw supplied value")
				}
			}
		})
	}
}

func TestSEC030ReceivePolicyLegacyZeroSemantics(t *testing.T) {
	for _, key := range []string{"GOKAPI_MAX_FILES_GUESTUPLOAD", "GOKAPI_MAX_SIZE_GUESTUPLOAD", "GOKAPI_MIN_FREE_SPACE"} {
		t.Setenv(key, "0")
	}
	t.Setenv("GOKAPI_UPLOAD_BODY_SLOTS", "4")
	t.Setenv("GOKAPI_UPLOAD_RECEIVE_MB", "12288")
	got := New()
	if got.MaxFilesGuestUpload != 0 || got.MaxSizeGuestUploadMb != 0 || got.MinFreeSpaceMB != 0 {
		t.Fatal("new reception policy changed legacy zero semantics")
	}
}
