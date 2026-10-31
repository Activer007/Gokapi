package models

import (
	"strings"
	"unicode/utf8"
)

// Delivery organizes existing ordinary shares. It never owns their blobs.
type Delivery struct {
	Token     string   `json:"token"`
	OwnerID   int      `json:"ownerId"`
	Title     string   `json:"title"`
	Note      string   `json:"note"`
	FileIDs   []string `json:"fileIds"`
	CreatedAt int64    `json:"createdAt"`
	ExpiresAt int64    `json:"expiresAt"`
	RevokedAt int64    `json:"revokedAt"`
}

const MaxDeliveryFiles = 20

// ValidDeliveryToken checks the public capability's URL-safe representation.
// The creation service is responsible for generating at least 128 random bits.
func ValidDeliveryToken(token string) bool {
	if len(token) < 22 || len(token) > 128 {
		return false
	}
	for _, c := range token {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Valid checks bounded persistent structure; ownership and current file state
// must additionally be verified by the service before creating a delivery.
func (d Delivery) Valid() bool {
	if !ValidDeliveryToken(d.Token) || d.OwnerID <= 0 || strings.TrimSpace(d.Title) == "" ||
		!utf8.ValidString(d.Title) || utf8.RuneCountInString(d.Title) > 200 ||
		!utf8.ValidString(d.Note) || utf8.RuneCountInString(d.Note) > 2000 ||
		len(d.FileIDs) < 2 || len(d.FileIDs) > MaxDeliveryFiles || d.CreatedAt <= 0 ||
		d.ExpiresAt < 0 || d.RevokedAt < 0 {
		return false
	}
	seen := make(map[string]bool, len(d.FileIDs))
	for _, id := range d.FileIDs {
		if id == "" || len(id) > 256 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
