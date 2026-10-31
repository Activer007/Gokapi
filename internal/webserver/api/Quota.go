package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
)

type quotaSummaryOutput struct {
	Available  bool  `json:"available"`
	UsedBytes  int64 `json:"usedBytes"`
	UsedFiles  int64 `json:"usedFiles"`
	LimitBytes int64 `json:"limitBytes"`
	LimitFiles int64 `json:"limitFiles"`
	AsOf       int64 `json:"asOf"`
}

var errQuotaSummaryUnavailable = errors.New("quota summary unavailable")

// readQuotaSummary reports logical metadata usage, not deduplicated blob size
// or admission capacity. Reservations and instance limits can still prevent a
// subsequent write. The provider reads usage and ledger health consistently.
func readQuotaSummary(ctx context.Context, userID int, env environment.Environment,
	readUsage func(context.Context, int) (uploadquota.Usage, error)) (quotaSummaryOutput, error) {
	if ctx == nil || userID <= 0 || readUsage == nil || env.ValidateWholeUploadLimits() != nil {
		return quotaSummaryOutput{}, errQuotaSummaryUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return quotaSummaryOutput{}, errQuotaSummaryUnavailable
	}
	usage, err := readUsage(ctx, userID)
	if err != nil || ctx.Err() != nil || usage.Bytes < 0 || usage.Files < 0 {
		return quotaSummaryOutput{}, errQuotaSummaryUnavailable
	}
	const mib = 1024 * 1024
	return quotaSummaryOutput{Available: true, UsedBytes: usage.Bytes, UsedFiles: usage.Files,
		LimitBytes: env.UploadUserTotalMB * mib, LimitFiles: env.UploadUserMaxFiles, AsOf: time.Now().Unix()}, nil
}

func (p *paramQuotaSummary) ProcessParameter(r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("GET required")
	}
	p.Request = r
	return nil
}

func apiQuotaSummary(w http.ResponseWriter, parsed requestParser, user models.User, _ models.ApiKey) {
	w.Header().Set("Cache-Control", "no-store")
	p := parsed.(*paramQuotaSummary)
	// No request parameter can select a different subject, including for
	// administrators with permission to list other users' uploads.
	summary, err := readQuotaSummary(p.Request.Context(), user.Id, configuration.GetEnvironment(), database.ReadUserUploadUsage)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(struct {
			Result    string `json:"Result"`
			Available bool   `json:"available"`
			Code      int    `json:"ErrorCode"`
			Message   string `json:"ErrorMessage"`
		}{Result: "error", Available: false, Code: errorcodes.InternalServer, Message: "Quota summary temporarily unavailable"})
		return
	}
	_ = json.NewEncoder(w).Encode(summary)
}
