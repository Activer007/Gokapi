package environment

import (
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// These are the configured defaults for the personal/trusted-team profile.
var sec030WholePolicy = []struct {
	key, field string
	value      int64
	bytes      bool
}{
	{"GOKAPI_UPLOAD_USER_TOTAL_MB", "UploadUserTotalMB", 5120, true},
	{"GOKAPI_UPLOAD_USER_MAX_FILES", "UploadUserMaxFiles", 1000, false},
	{"GOKAPI_UPLOAD_USER_ACTIVE", "UploadUserActive", 4, false},
	{"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB", "UploadInstanceTotalMB", 20480, true},
	{"GOKAPI_UPLOAD_INSTANCE_MAX_FILES", "UploadInstanceMaxFiles", 5000, false},
	{"GOKAPI_UPLOAD_INSTANCE_ACTIVE", "UploadInstanceActive", 8, false},
	{"GOKAPI_UPLOAD_STAGING_MB", "UploadStagingMB", 12288, true},
}

func sec030WholePolicyEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GOKAPI_UPLOAD_BODY_SLOTS", "4")
	t.Setenv("GOKAPI_UPLOAD_RECEIVE_MB", "12288")
	for _, field := range sec030WholePolicy {
		t.Setenv(field.key, strconv.FormatInt(field.value, 10))
	}
}

func sec030WholePolicyValue(t *testing.T, settings Environment, field string) int64 {
	t.Helper()
	value := reflect.ValueOf(settings).FieldByName(field)
	if !value.IsValid() || value.Kind() != reflect.Int64 {
		t.Fatalf("missing int64 whole-upload policy field %s", field)
	}
	return value.Int()
}

func sec030WholePolicyValidate(t *testing.T, settings Environment) error {
	t.Helper()
	validator, ok := any(settings).(interface{ ValidateWholeUploadLimits() error })
	if !ok {
		t.Fatal("missing ValidateWholeUploadLimits contract")
	}
	return validator.ValidateWholeUploadLimits()
}

func TestSEC030WholePolicyDefaults(t *testing.T) {
	sec030WholePolicyEnvironment(t)
	for _, field := range sec030WholePolicy {
		if err := os.Unsetenv(field.key); err != nil {
			t.Fatal(err)
		}
	}
	settings := New()
	if !settings.IsParsed() {
		t.Fatal("default policy failed environment parsing")
	}
	for _, field := range sec030WholePolicy {
		if got := sec030WholePolicyValue(t, settings, field.field); got != field.value {
			t.Fatalf("%s default=%d; want %d", field.field, got, field.value)
		}
		definition, ok := reflect.TypeOf(settings).FieldByName(field.field)
		if !ok || definition.Tag.Get("onlyPositive") != "" || definition.Tag.Get("minValue") != "" {
			t.Fatalf("%s must reject invalid explicit values instead of normalizing them", field.field)
		}
	}
	if err := sec030WholePolicyValidate(t, settings); err != nil {
		t.Fatalf("candidate defaults rejected by explicit validation: %v", err)
	}
}

func TestSEC030WholePolicyConfiguredValues(t *testing.T) {
	sec030WholePolicyEnvironment(t)
	values := []int64{1024, 3, 2, 4096, 7, 6, 2048}
	for i, field := range sec030WholePolicy {
		t.Setenv(field.key, strconv.FormatInt(values[i], 10))
	}
	settings := New()
	if !settings.IsParsed() {
		t.Fatal("valid configured whole-upload policy failed parsing")
	}
	for i, field := range sec030WholePolicy {
		if got := sec030WholePolicyValue(t, settings, field.field); got != values[i] {
			t.Fatalf("%s=%d; want configured value %d", field.field, got, values[i])
		}
	}
	if err := sec030WholePolicyValidate(t, settings); err != nil {
		t.Fatalf("valid configured policy rejected: %v", err)
	}
}

func TestSEC030WholePolicyByteConversionBoundary(t *testing.T) {
	for _, field := range sec030WholePolicy {
		if !field.bytes {
			continue
		}
		t.Run(field.field, func(t *testing.T) {
			sec030WholePolicyEnvironment(t)
			maximum := int64(math.MaxInt64 / (1024 * 1024))
			t.Setenv(field.key, strconv.FormatInt(maximum, 10))
			settings := New()
			if !settings.IsParsed() || sec030WholePolicyValue(t, settings, field.field) != maximum {
				t.Fatal("largest representable positive MiB capacity was not preserved")
			}
			if err := sec030WholePolicyValidate(t, settings); err != nil {
				t.Fatalf("representable MiB capacity rejected: %v", err)
			}
		})
	}
}

func TestSEC030WholePolicyNewFailsClosedWithoutValueEcho(t *testing.T) {
	for _, field := range sec030WholePolicy {
		values := []struct{ name, value string }{
			{"zero", "0"},
			{"negative", "-1"},
			{"empty", ""},
			{"integer-overflow", "9223372036854775808"},
			{"synthetic-sensitive-value", "SEC030_SYNTHETIC_WHOLE_VALUE_MUST_NOT_APPEAR"},
		}
		if field.bytes {
			values = append(values, struct{ name, value string }{
				"byte-conversion-overflow", strconv.FormatInt(math.MaxInt64/(1024*1024)+1, 10),
			})
		}
		for _, value := range values {
			t.Run(field.field+"/"+value.name, func(t *testing.T) {
				sec030WholePolicyEnvironment(t)
				t.Setenv(field.key, value.value)
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
					t.Fatal("invalid whole-upload capacity did not fail startup")
				}
				if settings.IsParsed() {
					t.Fatal("failed startup returned an apparently valid environment")
				}
				if value.name == "synthetic-sensitive-value" || strings.Contains(value.name, "overflow") {
					if strings.Contains(output, value.value) {
						t.Fatal("whole-upload configuration error echoed the raw supplied value")
					}
				}
			})
		}
	}
}

func TestSEC030WholePolicyExplicitValidation(t *testing.T) {
	sec030WholePolicyEnvironment(t)
	settings := New()
	for _, field := range sec030WholePolicy {
		invalidValues := []int64{0, -1}
		if field.bytes {
			invalidValues = append(invalidValues, math.MaxInt64/(1024*1024)+1)
		}
		for _, invalid := range invalidValues {
			t.Run(field.field+"/"+strconv.FormatInt(invalid, 10), func(t *testing.T) {
				copy := settings
				target := reflect.ValueOf(&copy).Elem().FieldByName(field.field)
				if !target.IsValid() || target.Kind() != reflect.Int64 {
					t.Fatalf("missing int64 whole-upload policy field %s", field.field)
				}
				target.SetInt(invalid)
				if err := sec030WholePolicyValidate(t, copy); err == nil {
					t.Fatal("explicit validation accepted an invalid whole-upload capacity")
				}
				if target.Int() != invalid {
					t.Fatal("explicit validation silently normalized a rejected value")
				}
			})
		}
	}
}

func TestSEC030WholePolicyLegacyZeroAndReceiveValidation(t *testing.T) {
	sec030WholePolicyEnvironment(t)
	for _, key := range []string{"GOKAPI_MAX_FILES_GUESTUPLOAD", "GOKAPI_MAX_SIZE_GUESTUPLOAD", "GOKAPI_MIN_FREE_SPACE"} {
		t.Setenv(key, "0")
	}
	settings := New()
	if !settings.IsParsed() || settings.MaxFilesGuestUpload != 0 || settings.MaxSizeGuestUploadMb != 0 || settings.MinFreeSpaceMB != 0 {
		t.Fatal("whole-upload policy changed the legacy guest or free-space zero semantics")
	}
	// Existing reception-only callers construct this value directly. Validation
	// of the new policy must not turn their omitted whole-upload fields into an
	// unrelated failure in ValidateReceiveLimits or generic integer enforcement.
	receptionOnly := Environment{UploadBodySlots: 4, UploadReceiveMB: 12288}
	if err := receptionOnly.ValidateReceiveLimits(); err != nil {
		t.Fatalf("whole-upload policy changed reception-only validation: %v", err)
	}
	if err := enforceIntLimits(&receptionOnly); err != nil {
		t.Fatalf("whole-upload policy changed existing synthetic environment enforcement: %v", err)
	}
}
