//go:build test

package api

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"
)

func TestUINameHeaderEncoding(t *testing.T) {
	for _, tc := range []struct {
		newParser func() requestParser
		header    string
		value     func(requestParser) string
	}{
		{func() requestParser { return (&paramUserCreate{}).New() }, "username", func(p requestParser) string { return p.(*paramUserCreate).Username }},
		{func() requestParser { return (&paramAuthCreate{}).New() }, "friendlyName", func(p requestParser) string { return p.(*paramAuthCreate).FriendlyName }},
		{func() requestParser { return (&paramAuthFriendlyName{}).New() }, "friendlyName", func(p requestParser) string { return p.(*paramAuthFriendlyName).FriendlyName }},
	} {
		for _, text := range []string{"Legacy ASCII", "中文名称😀"} {
			input := text
			if text != "Legacy ASCII" {
				input = "base64:" + base64.StdEncoding.EncodeToString([]byte(text))
			}
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set(tc.header, input)
			r.Header.Set("targetKey", "fixture")
			p := tc.newParser()
			if err := p.ParseRequest(r); err != nil {
				t.Fatal(err)
			}
			if err := p.ProcessParameter(r); err != nil {
				t.Fatal(err)
			}
			if tc.value(p) != text {
				t.Errorf("header %s did not round-trip", tc.header)
			}
		}
		for _, invalid := range []string{"base64:%", "base64:" + base64.StdEncoding.EncodeToString([]byte("line\nbreak")), "base64:" + base64.StdEncoding.EncodeToString([]byte{255})} {
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set(tc.header, invalid)
			r.Header.Set("targetKey", "fixture")
			p := tc.newParser()
			err := p.ParseRequest(r)
			if err == nil {
				err = p.ProcessParameter(r)
			}
			if err == nil {
				t.Errorf("invalid %s accepted", tc.header)
			}
		}
	}
}
