package models

// Presign is a struct used for generating presigned URLs
type Presign struct {
	Id       string
	FileIds  []string
	Expiry   int64
	Filename string
	Archive  bool // The issuing endpoint requested a ZIP, including a single-file ZIP.
	// Management authority is rechecked at consumption; never serialized into
	// the public URL. Presigns are in-memory and are not a database schema.
	UserId   int
	ApiKeyId string
}

// IsArchive preserves older multi-file grants that predate the explicit mode.
func (p Presign) IsArchive() bool {
	return p.Archive || len(p.FileIds) > 1
}
