package models

// Presign is a struct used for generating presigned URLs
type Presign struct {
	Id       string
	FileIds  []string
	Expiry   int64
	Filename string
	// Management authority is rechecked at consumption; never serialized into
	// the public URL. Presigns are in-memory and are not a database schema.
	UserId   int
	ApiKeyId string
}
