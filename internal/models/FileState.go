package models

// FileStateOperation selects a server-internal conditional metadata patch.
// These values are not persisted or exposed as an HTTP payload.
type FileStateOperation uint8

const (
	FileStateExpire FileStateOperation = iota + 1
	FileStateSchedule
	FileStateCancel
	FileStatePassword
)

// FileStatePatch carries the caller's checked snapshot. The provider compares
// it with the current row before changing only the selected control fields.
type FileStatePatch struct {
	Expected        File               `json:"-"`
	Operation       FileStateOperation `json:"-"`
	PendingDeletion int64              `json:"-"`
	PasswordHash    string             `json:"-"`
}
