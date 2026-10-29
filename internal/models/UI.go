package models

import (
	"fmt"
	"net/url"
	"strings"
)

// UIConfig contains optional deployment-authored presentation settings.
// A missing UI object keeps the original English language and product footer.
type UIConfig struct {
	DefaultLanguage string       `json:"DefaultLanguage,omitempty"`
	Title           string       `json:"Title,omitempty"`
	FooterText      string       `json:"FooterText,omitempty"`
	FooterLinks     []FooterLink `json:"FooterLinks,omitempty"`
}

type FooterLink struct {
	Text string `json:"Text"`
	URL  string `json:"URL"`
}

func (u *UIConfig) Validate() error {
	if u == nil {
		return nil
	}
	if u.DefaultLanguage != "" && u.DefaultLanguage != "en" && u.DefaultLanguage != "zh-CN" {
		return fmt.Errorf("UI.DefaultLanguage must be en or zh-CN")
	}
	for index, link := range u.FooterLinks {
		parsed, err := url.Parse(link.URL)
		if err != nil || strings.TrimSpace(link.Text) == "" || strings.ContainsAny(link.URL, "\\\r\n\t") {
			return fmt.Errorf("UI.FooterLinks[%d] requires text and a valid URL", index)
		}
		absolute := (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil
		local := strings.HasPrefix(link.URL, "/") && !strings.HasPrefix(link.URL, "//") && parsed.Scheme == "" && parsed.Host == ""
		if !absolute && !local {
			return fmt.Errorf("UI.FooterLinks[%d].URL must be http(s) or a site-relative path starting with /", index)
		}
	}
	return nil
}
