package storage

import (
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/uploadquota"
)

// A duplicate has no new body, active upload or staging. Use the same startup
// logical capacities as uploads; current request MaxFiles is resolved in TX/CAS.
func duplicateLimits() (uploadquota.Limits, error) {
	env := configuration.GetEnvironment()
	if err := env.ValidateWholeUploadLimits(); err != nil {
		return uploadquota.Limits{}, err
	}
	if err := env.ValidateRequestUploadLimits(); err != nil {
		return uploadquota.Limits{}, err
	}
	const mib = 1 << 20
	return uploadquota.Limits{
		User:     uploadquota.Usage{Bytes: env.UploadUserTotalMB * mib, Files: env.UploadUserMaxFiles},
		Instance: uploadquota.Usage{Bytes: env.UploadInstanceTotalMB * mib, Files: env.UploadInstanceMaxFiles},
		Request:  uploadquota.Usage{Bytes: env.UploadRequestTotalMB * mib, Files: env.UploadInstanceMaxFiles},
	}, nil
}
