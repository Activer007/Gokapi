//go:build test && security_reclaim && security_cleanup && !noaws && !awsmock

package securityfixture_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// Each case uses a loopback S3 service and a fresh SQLite database. Faults start
// only after a normal replacement has committed, so setup failures cannot count
// as evidence that an old-content retry anchor was retained.
func TestSEC041OldContentS3RetryKeepsReplacement(t *testing.T) {
	for _, mode := range []string{"delete-failed", "head-unknown", "delete-ignored"} {
		t.Run(mode, func(t *testing.T) {
			sec040CleanupOpen(t, t.TempDir())
			remote := securityfixture.NewS3(t)
			sec040CleanupLogin(t, remote.Server.URL)
			oldPayload := []byte("synthetic SEC041 original " + mode)
			newPayload := []byte("synthetic SEC041 replacement " + mode)
			target := sec040CleanupFile("reclaim-target", "security-fixture", oldPayload)
			source := sec040CleanupFile("reclaim-source", "security-fixture", newPayload)
			target.UnlimitedTime, source.UnlimitedTime = true, true
			target.ExpireAt, source.ExpireAt = time.Now().Add(time.Hour).Unix(), time.Now().Add(time.Hour).Unix()
			source.UserId = 7003
			sec040CleanupUpload(t, target, oldPayload)
			sec040CleanupUpload(t, source, newPayload)
			sec040CleanupSave(target)
			sec040CleanupSave(source)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			replaced, err := storage.ReplaceFileContext(ctx, target, source, false)
			cancel()
			if err != nil {
				t.Fatal("SEC041_SETUP normal replacement failed", err)
			}
			expected := target
			expected.Name, expected.Size, expected.SHA1 = source.Name, source.Size, source.SHA1
			expected.ContentType, expected.AwsBucket, expected.SizeBytes = source.ContentType, source.AwsBucket, source.SizeBytes
			expected.Encryption = source.Encryption
			if !reflect.DeepEqual(replaced, expected) {
				t.Fatal("SEC041_SETUP replacement returned unexpected metadata")
			}
			candidates, err := database.ReadBlobCandidates(t.Context())
			if err != nil || len(candidates) != 1 || candidates[0].SHA1 != target.SHA1 || candidates[0].AwsBucket != target.AwsBucket {
				t.Fatal("SEC041_ANCHOR successful replacement lost its old locator", err)
			}
			beforeHeads, beforeDeletes := remote.HeadAttempts.Load(), remote.DeleteAttempts.Load()
			switch mode {
			case "delete-failed":
				remote.FailDelete.Store(true)
			case "head-unknown":
				remote.FailHead.Store(true)
			case "delete-ignored":
				remote.IgnoreDelete.Store(true)
			}
			attempt, stopAttempt := context.WithTimeout(t.Context(), 25*time.Second)
			_, cleanupErr := storage.CleanUpContext(attempt)
			stopAttempt()
			if cleanupErr == nil || remote.HeadAttempts.Load() <= beforeHeads {
				t.Fatal("SEC041_FAULT cleanup did not report the exercised object uncertainty")
			}
			if mode == "head-unknown" {
				if remote.DeleteAttempts.Load() != beforeDeletes {
					t.Fatal("SEC041_UNKNOWN an uncertain HEAD authorized DELETE")
				}
			} else if remote.DeleteAttempts.Load() <= beforeDeletes {
				t.Fatal("SEC041_FAULT DELETE fault was not reached")
			}
			remote.FailDelete.Store(false)
			remote.FailHead.Store(false)
			remote.IgnoreDelete.Store(false)
			if got, err := database.ReadBlobCandidates(t.Context()); err != nil || !reflect.DeepEqual(got, candidates) {
				t.Fatal("SEC041_RETAIN uncertain physical result lost or changed the retry intent", err)
			}
			sec040CleanupObject(t, target, int64(len(oldPayload)), true)
			sec041ReplacementUnchanged(t, remote.Server.URL, expected, source, newPayload)
			sec040CleanupAttempt(t, false)
			if got, err := database.ReadBlobCandidates(t.Context()); err != nil || len(got) != 0 {
				t.Fatal("SEC041_RECOVERY confirmed cleanup did not acknowledge the old intent", err)
			}
			sec040CleanupObject(t, target, 0, false)
			sec041ReplacementUnchanged(t, remote.Server.URL, expected, source, newPayload)
			completedDeletes := remote.DeleteAttempts.Load()
			sec040CleanupAttempt(t, false)
			if remote.DeleteAttempts.Load() != completedDeletes {
				t.Fatal("SEC041_IDEMPOTENT an acknowledged intent repeated physical deletion")
			}
		})
	}
}

func sec041ReplacementUnchanged(t *testing.T, endpoint string, target, source models.File, payload []byte) {
	t.Helper()
	for _, expected := range []models.File{target, source} {
		stored, found, err := database.ReadRecoveryFile(t.Context(), expected.Id)
		if err != nil || !found || !reflect.DeepEqual(stored, expected) {
			t.Fatal("SEC041_PRESERVE replacement or source metadata changed during old-content cleanup", err)
		}
		if id, found := database.GetHotlink(expected.HotlinkId); !found || id != expected.Id {
			t.Fatal("SEC041_PRESERVE old-content cleanup changed a current hotlink")
		}
	}
	sess, err := session.NewSession(&awssdk.Config{
		Endpoint: awssdk.String(endpoint), Region: awssdk.String("us-east-1"), S3ForcePathStyle: awssdk.Bool(true),
		Credentials: credentials.NewStaticCredentials("synthetic", "synthetic", ""), MaxRetries: awssdk.Int(0),
	})
	if err != nil {
		t.Fatal("SEC041_CONTENT could not prepare the loopback SDK", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	object, err := s3.New(sess).GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: awssdk.String(target.AwsBucket), Key: awssdk.String(target.SHA1)})
	if err != nil {
		t.Fatal("SEC041_CONTENT could not read the replacement object", err)
	}
	defer object.Body.Close()
	content, err := io.ReadAll(io.LimitReader(object.Body, int64(len(payload))+1))
	if err != nil || !bytes.Equal(content, payload) {
		t.Fatal("SEC041_CONTENT replacement bytes changed during cleanup", err)
	}
}
