package storage

import (
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// Metadata operations have no new body, active upload or staging. Use the same
// startup logical capacities as uploads; each action checks only its delta.
func metadataLimits() (uploadquota.Limits, error) {
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
