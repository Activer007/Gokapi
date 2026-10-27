//go:build test && !noaws && !awsmock

package securityfixture_test

import (
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/filesystem/s3filesystem/aws"
	"github.com/forceu/gokapi/internal/test/securityfixture"
)

// Exercise the actual SDK driver against loopback, not the awsmock stub.
func TestM0S3DeleteFailureRecovery(t *testing.T) {
	f := securityfixture.NewS3(t)
	if !aws.Init(models.AwsConfig{Bucket: "security-fixture", Region: "us-east-1", Endpoint: f.Server.URL, KeyId: "synthetic", KeySecret: "synthetic"}) {
		t.Fatal("fixture login failed")
	}
	t.Cleanup(aws.LogOut)
	file := models.File{Id: "synthetic-file", SHA1: "synthetic-blob", AwsBucket: "security-fixture"}
	if _, err := aws.Upload(strings.NewReader("fixture bytes"), file); err != nil {
		t.Fatal(err)
	}
	f.FailDelete.Store(true)
	if ok, err := aws.DeleteObject(file); err == nil || ok {
		t.Fatal("driver did not propagate deletion failure")
	}
	if exists, _, err := aws.FileExists(file); err != nil || !exists {
		t.Fatal("failed deletion lost object")
	}
	if f.DeleteAttempts.Load() == 0 {
		t.Fatal("fault injection was not exercised")
	}
	f.FailDelete.Store(false)
	if ok, err := aws.DeleteObject(file); err != nil || !ok {
		t.Fatal("retry did not recover")
	}
	if exists, _, err := aws.FileExists(file); err != nil || exists {
		t.Fatal("object remained after successful deletion")
	}
}
