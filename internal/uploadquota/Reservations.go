package uploadquota

import (
	"errors"
	"fmt"

	"github.com/forceu/gokapi/internal/models"
)

const (
	LedgerVersion           = 1
	MaxRecords              = 4096
	MaxUserRecords          = 1024
	MaxRequestRecords       = 256
	MaxLedgerBytes          = 4 * 1024 * 1024
	MaxIDLength             = 128
	MaxLifetime       int64 = 24 * 60 * 60
	ReceiptLifetime   int64 = 24 * 60 * 60
)

var (
	ErrInvalid = errors.New("invalid upload quota data")
	ErrQuota   = errors.New("upload quota exceeded")
	// These retain ErrQuota identity for callers that only need a capacity
	// decision, while HTTP admission can distinguish temporary backpressure.
	ErrActive     = fmt.Errorf("%w: active upload capacity", ErrQuota)
	ErrStaging    = fmt.Errorf("%w: staging capacity", ErrQuota)
	ErrConflict   = errors.New("upload reservation conflict")
	ErrExpired    = errors.New("upload reservation expired")
	ErrNotFound   = errors.New("upload reservation not found")
	ErrInProgress = errors.New("upload completion already claimed")
	ErrFull       = errors.New("upload receipt capacity reached")
	ErrRetry      = errors.New("upload quota transaction retry limit reached")
	ErrSchema     = errors.New("upload quota schema is unavailable")
)

type State string

const (
	Reserved  State = "reserved"
	Uploading State = "uploading"
	Committed State = "committed"
	Aborted   State = "aborted"
	Expired   State = "expired"
)

// Reservation contains bounded, server-owned identifiers, never filesystem paths
// or credentials. Expired active records retain capacity until cleanup is confirmed.
type Reservation struct {
	ID               string
	UserID           int
	RequestID        string
	BytesKnown       bool
	InitiallyUnbound bool
	Bytes            int64
	StagingBytes     int64
	StagingReleased  bool
	CreatedAt        int64
	ExpiresAt        int64
	FinishedAt       int64
	State            State
	Completing       bool
	FileID           string
	CommitDigest     string
}

type Ledger struct {
	Version int
	Records map[string]Reservation
}

// Limits are literal capacities. Legacy zero-as-unlimited settings must be
// translated by the trusted caller while retaining finite enclosing limits.
type Limits struct {
	User           Usage
	Request        Usage
	Instance       Usage
	UserActive     int64
	RequestActive  int64
	InstanceActive int64
	StagingBytes   int64
}

type Action string

const (
	Reserve           Action = "reserve"
	Bind              Action = "bind"
	Claim             Action = "claim"
	Commit            Action = "commit"
	Abort             Action = "abort"
	ReleaseStaging    Action = "release-staging"
	DuplicateMetadata Action = "duplicate-metadata"
)

// Change is an internal database command, not a public request parser. Upload
// carries the authenticated scope; Reserve/Bind additionally supply a declaration.
// CleanupConfirmed may only be set after I/O has stopped and staged data is gone.
type Change struct {
	Action             Action
	Upload             Reservation
	Limits             Limits
	Now                int64
	FileID             string
	File               *models.File
	Terminal           State
	CleanupConfirmed   bool
	CheckRequestPolicy bool
	// Source is a trusted authorization snapshot, rechecked inside the provider
	// transaction for an instantaneous metadata duplicate. It is not persisted.
	Source *models.File
}

func EmptyLedger() Ledger {
	return Ledger{Version: LedgerVersion, Records: make(map[string]Reservation)}
}

func (ledger Ledger) HasOutstanding() bool {
	for _, record := range ledger.Records {
		if record.State == Reserved || record.State == Uploading || !record.StagingReleased {
			return true
		}
	}
	return false
}
