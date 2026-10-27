//go:build test && security_red && !noaws && !awsmock

package securityfixture_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/storage/filesystem/s3filesystem/aws"
	"github.com/forceu/gokapi/internal/test/securityfixture"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

// Desired invariant, intentionally RED at the recorded M0 baseline. Every byte
// and metadata row belongs to a temporary directory or an in-memory S3 endpoint.
func TestSEC040FailedDeletionKeepsManagementRecord(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOKAPI_CONFIG_DIR", "test")
	t.Setenv("GOKAPI_DATA_DIR", "test/data")
	testconfiguration.Create(false)
	configuration.Load()
	database.Connect(models.DbConnection{Type: 0, HostUrl: filepath.Join(t.TempDir(), "fixture.sqlite")})
	t.Cleanup(database.Close)
	database.SaveUser(models.User{Id: 7002, Name: "synthetic-owner", UserLevel: models.UserLevelUser}, false)
	f := securityfixture.NewS3(t)
	if !aws.Init(models.AwsConfig{Bucket: "security-fixture", Region: "us-east-1", Endpoint: f.Server.URL, KeyId: "synthetic", KeySecret: "synthetic"}) {
		t.Fatal("fixture login failed")
	}
	t.Cleanup(aws.LogOut)
	file := models.File{Id: "synthetic-delete", SHA1: "synthetic-delete-blob", UserId: 7002, AwsBucket: "security-fixture", ExpireAt: 1, UnlimitedDownloads: true}
	if _, err := aws.Upload(strings.NewReader("fixture bytes"), file); err != nil {
		t.Fatal(err)
	}
	database.SaveMetaData(file)
	f.FailDelete.Store(true)
	storage.CleanUp(false)
	if f.DeleteAttempts.Load() == 0 {
		t.Fatal("fixture failure was not exercised")
	}
	if exists, _, err := aws.FileExists(file); err != nil || !exists {
		t.Fatal("fixture object unexpectedly absent")
	}
	if _, exists := database.GetMetaDataById(file.Id); !exists {
		t.Fatal("SEC-040: deletion failed but management metadata was removed")
	}
}
