package models

import (
	"testing"

	"github.com/Activer007/Gokapi/internal/test"
)

func TestIsEmpty(t *testing.T) {
	status := UploadStatus{}
	test.IsEqualBool(t, status.IsForUser(0), false)
	test.IsEqualBool(t, status.IsForUser(1), false)
}

func TestIsPopulated(t *testing.T) {
	status := UploadStatus{UserId: 1}
	test.IsEqualBool(t, status.IsForUser(0), false)
	test.IsEqualBool(t, status.IsForUser(1), true)
	test.IsEqualBool(t, status.IsForUser(2), false)
}
