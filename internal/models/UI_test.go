package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUIConfigurationCompatibilityAndValidation(t *testing.T) {
	var old Configuration
	if err := json.Unmarshal([]byte(`{"PublicName":"Legacy"}`), &old); err != nil || old.UI != nil {
		t.Fatal("legacy configuration must not require UI")
	}
	if strings.Contains(old.ToString(), `"UI"`) {
		t.Fatal("legacy round trip must not add UI")
	}
	for _, language := range []string{"", "en", "zh-CN"} {
		if err := (&UIConfig{DefaultLanguage: language}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if (&UIConfig{DefaultLanguage: "fr"}).Validate() == nil {
		t.Fatal("unsupported configured language accepted")
	}
	for _, address := range []string{"https://example.com/help", "http://example.com", "/help?q=1"} {
		if err := (&UIConfig{FooterLinks: []FooterLink{{Text: "Help", URL: address}}}).Validate(); err != nil {
			t.Errorf("valid URL %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"javascript:alert(1)", "//example.com", "/\\example.com", "https://user:password@example.com", "", "ftp://example.com", "/help\n"} {
		if (&UIConfig{FooterLinks: []FooterLink{{Text: "Help", URL: address}}}).Validate() == nil {
			t.Errorf("invalid URL %q accepted", address)
		}
	}
	if (&UIConfig{FooterLinks: []FooterLink{{Text: " ", URL: "/help"}}}).Validate() == nil {
		t.Fatal("empty link text accepted")
	}
}
