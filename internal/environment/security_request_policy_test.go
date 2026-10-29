package environment

import (
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// These finite defaults follow the current implementation decision; this test
// does not imply that the user individually approved their numeric values.
var sec030RequestEnvironmentFields = []struct {
	key, field string
	value      int64
	bytes      bool
}{
	{"GOKAPI_UPLOAD_REQUEST_TOTAL_MB", "UploadRequestTotalMB", 20480, true},
	{"GOKAPI_UPLOAD_REQUEST_ACTIVE", "UploadRequestActive", 2, false},
}

func sec030RequestEnvironment(t *testing.T) {
	t.Helper()
	sec030WholePolicyEnvironment(t)
	for _, field := range sec030RequestEnvironmentFields {
		t.Setenv(field.key, strconv.FormatInt(field.value, 10))
	}
}

func sec030RequestEnvironmentValue(t *testing.T, settings Environment, field string) int64 {
	t.Helper()
	value := reflect.ValueOf(settings).FieldByName(field)
	if !value.IsValid() || value.Kind() != reflect.Int64 {
		t.Fatalf("missing int64 request policy field %s", field)
	}
	return value.Int()
}

func sec030RequestEnvironmentValidate(t *testing.T, settings Environment) error {
	t.Helper()
	validator, ok := any(settings).(interface{ ValidateRequestUploadLimits() error })
	if !ok {
		t.Fatal("missing ValidateRequestUploadLimits contract")
	}
	return validator.ValidateRequestUploadLimits()
}

func TestSEC030RequestEnvironmentDefaults(t *testing.T) {
	sec030RequestEnvironment(t)
	for _, field := range sec030RequestEnvironmentFields {
		if err := os.Unsetenv(field.key); err != nil {
			t.Fatal(err)
		}
	}
	settings := New()
	if !settings.IsParsed() {
		t.Fatal("request policy defaults failed environment parsing")
	}
	for _, field := range sec030RequestEnvironmentFields {
		if got := sec030RequestEnvironmentValue(t, settings, field.field); got != field.value {
			t.Fatalf("%s default=%d; want %d", field.field, got, field.value)
		}
		definition, ok := reflect.TypeOf(settings).FieldByName(field.field)
		if !ok || definition.Tag.Get("onlyPositive") != "" || definition.Tag.Get("minValue") != "" {
			t.Fatalf("%s must reject invalid values rather than silently normalize them", field.field)
		}
	}
	if err := sec030RequestEnvironmentValidate(t, settings); err != nil {
		t.Fatalf("request defaults rejected by explicit validation: %v", err)
	}
}

func TestSEC030RequestEnvironmentConfiguredValues(t *testing.T) {
	sec030RequestEnvironment(t)
	for _, field := range sec030RequestEnvironmentFields {
		t.Setenv(field.key, "3")
	}
	settings := New()
	if !settings.IsParsed() {
		t.Fatal("valid request policy failed environment parsing")
	}
	for _, field := range sec030RequestEnvironmentFields {
		if got := sec030RequestEnvironmentValue(t, settings, field.field); got != 3 {
			t.Fatalf("%s=%d; want explicitly configured 3", field.field, got)
		}
	}
	if err := sec030RequestEnvironmentValidate(t, settings); err != nil {
		t.Fatalf("valid request policy rejected: %v", err)
	}
}

func TestSEC030RequestEnvironmentByteBoundary(t *testing.T) {
	sec030RequestEnvironment(t)
	maximum := int64(math.MaxInt64 / (1024 * 1024))
	t.Setenv("GOKAPI_UPLOAD_REQUEST_TOTAL_MB", strconv.FormatInt(maximum, 10))
	settings := New()
	if !settings.IsParsed() || sec030RequestEnvironmentValue(t, settings, "UploadRequestTotalMB") != maximum {
		t.Fatal("largest representable request MiB capacity was not preserved")
	}
	if err := sec030RequestEnvironmentValidate(t, settings); err != nil {
		t.Fatalf("representable request MiB capacity rejected: %v", err)
	}
}

func TestSEC030RequestEnvironmentFailsClosedWithoutValueEcho(t *testing.T) {
	for _, field := range sec030RequestEnvironmentFields {
		cases := []struct{ name, value string }{
			{"zero", "0"},
			{"negative", "-1"},
			{"empty", ""},
			{"integer-overflow", "9223372036854775808"},
			{"synthetic-sensitive-value", "SEC030_SYNTHETIC_REQUEST_VALUE_MUST_NOT_APPEAR"},
		}
		if field.bytes {
			cases = append(cases, struct{ name, value string }{
				"byte-conversion-overflow", strconv.FormatInt(math.MaxInt64/(1024*1024)+1, 10),
			})
		}
		for _, tc := range cases {
			t.Run(field.field+"/"+tc.name, func(t *testing.T) {
				sec030RequestEnvironment(t)
				t.Setenv(field.key, tc.value)
				originalExit := osExit
				defer func() { osExit = originalExit }()
				exits := 0
				osExit = func(code int) {
					if code != 1 {
						t.Errorf("unexpected exit code %d", code)
					}
					exits++
				}
				var settings Environment
				output := receivePolicyOutput(t, func() { settings = New() })
				if exits == 0 {
					t.Fatal("invalid request capacity did not fail startup")
				}
				if settings.IsParsed() {
					t.Fatal("failed startup returned an apparently valid environment")
				}
				if tc.name == "synthetic-sensitive-value" || strings.Contains(tc.name, "overflow") {
					if strings.Contains(output, tc.value) {
						t.Fatal("request configuration error echoed the raw supplied value")
					}
				}
			})
		}
	}
}

func TestSEC030RequestEnvironmentExplicitValidation(t *testing.T) {
	sec030RequestEnvironment(t)
	settings := New()
	for _, field := range sec030RequestEnvironmentFields {
		invalid := []int64{0, -1}
		if field.bytes {
			invalid = append(invalid, math.MaxInt64/(1024*1024)+1)
		}
		for _, value := range invalid {
			t.Run(field.field+"/"+strconv.FormatInt(value, 10), func(t *testing.T) {
				copy := settings
				target := reflect.ValueOf(&copy).Elem().FieldByName(field.field)
				if !target.IsValid() || target.Kind() != reflect.Int64 {
					t.Fatalf("missing int64 request policy field %s", field.field)
				}
				target.SetInt(value)
				if err := sec030RequestEnvironmentValidate(t, copy); err == nil {
					t.Fatal("explicit request validation accepted an invalid capacity")
				}
				if target.Int() != value {
					t.Fatal("explicit request validation silently normalized rejected input")
				}
			})
		}
	}
}

func TestSEC030RequestEnvironmentLegacyZeroAndSyntheticCompatibility(t *testing.T) {
	sec030RequestEnvironment(t)
	for _, key := range []string{"GOKAPI_MAX_FILES_GUESTUPLOAD", "GOKAPI_MAX_SIZE_GUESTUPLOAD", "GOKAPI_MIN_FREE_SPACE"} {
		t.Setenv(key, "0")
	}
	settings := New()
	if !settings.IsParsed() || settings.MaxFilesGuestUpload != 0 || settings.MaxSizeGuestUploadMb != 0 || settings.MinFreeSpaceMB != 0 {
		t.Fatal("request policy changed legacy guest or free-space zero semantics")
	}
	wholeOnly := Environment{
		UploadBodySlots: 4, UploadReceiveMB: 12288,
		UploadUserTotalMB: 20480, UploadUserMaxFiles: 1000, UploadUserActive: 4,
		UploadInstanceTotalMB: 102400, UploadInstanceMaxFiles: 5000, UploadInstanceActive: 8,
		UploadStagingMB: 12288,
	}
	if err := wholeOnly.ValidateWholeUploadLimits(); err != nil {
		t.Fatalf("request policy changed synthetic whole-only validation: %v", err)
	}
	if err := wholeOnly.ValidateReceiveLimits(); err != nil {
		t.Fatalf("request policy changed synthetic receive validation: %v", err)
	}
	if err := enforceIntLimits(&wholeOnly); err != nil {
		t.Fatalf("request policy changed generic synthetic enforcement: %v", err)
	}
}
