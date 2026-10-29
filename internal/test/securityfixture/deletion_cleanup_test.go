//go:build test && security_cleanup && !noaws && !awsmock

package securityfixture_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/configupgrade"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
)

const sec040CleanupChild = "SUPASHARE_SEC040_CLEANUP_CHILD"

func TestSEC040CleanupCancelled(t *testing.T) {
	sec040CleanupOpen(t, t.TempDir())
	remote := securityfixture.NewS3(t)
	sec040CleanupLogin(t, remote.Server.URL)
	payload := []byte("synthetic SEC040 cancellation")
	file := sec040CleanupFile("cancelled", "security-fixture", payload)
	sec040CleanupUpload(t, file, payload)
	sec040CleanupSave(file)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := storage.CleanUpContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("SEC040_CANCEL cleanup replaced a cancelled context")
	}
	if _, err := storage.CleanUpContext(nil); err == nil {
		t.Fatal("SEC040_CANCEL cleanup accepted a missing context")
	}
	if deleted, err := aws.DeleteObjectContext(ctx, file); deleted || !errors.Is(err, context.Canceled) {
		t.Fatal("SEC040_CANCEL object deletion replaced a cancelled context")
	}
	if deleted, err := aws.DeleteObjectContext(nil, file); deleted || err == nil {
		t.Fatal("SEC040_CANCEL object deletion accepted a missing context")
	}
	if remote.DeleteAttempts.Load() != 0 {
		t.Fatal("SEC040_CANCEL a cancelled caller attempted physical deletion")
	}
	sec040CleanupRetained(t, file)
	sec040CleanupObject(t, file, int64(len(payload)), true)
}

// All failures occur against a loopback object store and disposable SQLite.
// Result counters deliberately do not substitute for the actual stored state.
func TestSEC040CleanupS3UncertainResults(t *testing.T) {
	for _, mode := range []string{"delete-failed", "head-unknown", "delete-ignored"} {
		t.Run(mode, func(t *testing.T) {
			sec040CleanupOpen(t, t.TempDir())
			remote := securityfixture.NewS3(t)
			sec040CleanupLogin(t, remote.Server.URL)
			payload := []byte("synthetic SEC040 " + mode)
			file := sec040CleanupFile(mode, "security-fixture", payload)
			sec040CleanupUpload(t, file, payload)
			sec040CleanupSave(file)
			var independent *models.File
			if mode == "delete-failed" {
				localPayload := []byte("synthetic SEC040 independent local object")
				local := sec040CleanupFile("independent-local", "", localPayload)
				// ReadCleanupIDs sorts IDs: this row follows the failing remote
				// entry, so stopping the batch on its first error cannot pass.
				local.Id, local.HotlinkId, local.UserId = "zz-sec040-independent", "zz-sec040-independent.png", 7003
				if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, local.SHA1), localPayload, 0600); err != nil {
					t.Fatal("SEC040_SETUP could not write the independent local object")
				}
				sec040CleanupSave(local)
				independent = &local
			}
			beforeHeads := remote.HeadAttempts.Load()
			switch mode {
			case "delete-failed":
				remote.FailDelete.Store(true)
			case "head-unknown":
				remote.FailHead.Store(true)
			case "delete-ignored":
				remote.IgnoreDelete.Store(true)
			}
			sec040CleanupAttempt(t, true)
			if remote.HeadAttempts.Load() <= beforeHeads {
				t.Fatal("SEC040_FAULT cleanup did not inspect the synthetic object")
			}
			if mode == "head-unknown" {
				if remote.DeleteAttempts.Load() != 0 {
					t.Fatal("SEC040_UNKNOWN existence uncertainty caused a DELETE")
				}
			} else if remote.DeleteAttempts.Load() == 0 {
				t.Fatal("SEC040_FAULT DELETE fault was not exercised")
			}
			remote.FailHead.Store(false)
			sec040CleanupRetained(t, file)
			sec040CleanupObject(t, file, int64(len(payload)), true)
			if independent != nil {
				sec040CleanupRetired(t, *independent)
				if _, err := os.Lstat(filepath.Join(configuration.Get().DataDir, independent.SHA1)); !os.IsNotExist(err) {
					t.Fatal("SEC040_BATCH one failed remote blocked an independent safe local deletion")
				}
			}
		})
	}
}

// The first process actually exits after its failed cleanup. The parent only
// opens the database afterwards, while the same loopback object survives.
func TestSEC040CleanupRestart(t *testing.T) {
	if os.Getenv(sec040CleanupChild) == "1" {
		sec040CleanupOpen(t, os.Getenv("SUPASHARE_SEC040_CLEANUP_ROOT"))
		sec040CleanupLogin(t, os.Getenv("SUPASHARE_SEC040_CLEANUP_ENDPOINT"))
		payload := []byte("synthetic SEC040 restart object")
		file := sec040CleanupFile("restart", "security-fixture", payload)
		sec040CleanupUpload(t, file, payload)
		sec040CleanupSave(file)
		sec040CleanupAttempt(t, true)
		sec040CleanupRetained(t, file)
		sec040CleanupObject(t, file, int64(len(payload)), true)
		return
	}
	root := t.TempDir()
	remote := securityfixture.NewS3(t)
	remote.FailDelete.Store(true)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("SEC040_SETUP could not locate isolated child executable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestSEC040CleanupRestart$", "-test.count=1")
	child.Dir = root
	policy := map[string]string{
		sec040CleanupChild:                  "1",
		"SUPASHARE_SEC040_CLEANUP_ROOT":     root,
		"SUPASHARE_SEC040_CLEANUP_ENDPOINT": remote.Server.URL,
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := policy[strings.ToUpper(name)]; !replaced {
			child.Env = append(child.Env, entry)
		}
	}
	for name, value := range policy {
		child.Env = append(child.Env, name+"="+value)
	}
	output, childErr := child.CombinedOutput()
	// Preserve diagnostics only inside the disposable fixture, never in test
	// output: SDK failures can contain a private endpoint or object identifier.
	if err := os.WriteFile(filepath.Join(root, "child-private.log"), output, 0600); err != nil {
		t.Fatal("SEC040_SETUP could not retain isolated child diagnostics")
	}
	if childErr != nil {
		t.Fatal("SEC040_RESTART first process did not finish its expected failed-cleanup contract")
	}
	if remote.DeleteAttempts.Load() == 0 {
		t.Fatal("SEC040_RESTART first process never exercised the DELETE failure")
	}
	sec040CleanupOpen(t, root)
	sec040CleanupLogin(t, remote.Server.URL)
	payload := []byte("synthetic SEC040 restart object")
	file := sec040CleanupFile("restart", "security-fixture", payload)
	sec040CleanupRetained(t, file)
	sec040CleanupObject(t, file, int64(len(payload)), true)
	before := remote.DeleteAttempts.Load()
	remote.FailDelete.Store(false)
	sec040CleanupAttempt(t, false)
	if remote.DeleteAttempts.Load() <= before {
		t.Fatal("SEC040_RESTART fresh process did not retry the retained object")
	}
	sec040CleanupRetired(t, file)
	sec040CleanupObject(t, file, 0, false)
}

func TestSEC040CleanupSharedAcrossOwners(t *testing.T) {
	sec040CleanupOpen(t, t.TempDir())
	remote := securityfixture.NewS3(t)
	sec040CleanupLogin(t, remote.Server.URL)
	payload := []byte("synthetic SEC040 shared object")
	expired := sec040CleanupFile("shared-expired", "security-fixture", payload)
	live := expired
	live.Id, live.HotlinkId, live.UserId = "sec040-shared-live", "sec040-shared-live.png", 7003
	live.UnlimitedTime = true
	sec040CleanupUpload(t, expired, payload)
	sec040CleanupSave(expired)
	sec040CleanupSave(live)
	sec040CleanupAttempt(t, false)
	sec040CleanupRetired(t, expired)
	if remote.DeleteAttempts.Load() != 0 {
		t.Fatal("SEC040_SHARED another owner's live reference did not prevent DELETE")
	}
	if stored, found := database.GetMetaDataById(live.Id); !found || stored.UserId != live.UserId || stored.SHA1 != live.SHA1 {
		t.Fatal("SEC040_SHARED cleanup changed the surviving owner's metadata")
	}
	if id, found := database.GetHotlink(live.HotlinkId); !found || id != live.Id {
		t.Fatal("SEC040_SHARED cleanup changed the surviving hotlink")
	}
	if _, found := storage.GetFile(live.Id); !found {
		t.Fatal("SEC040_SHARED surviving owner could not retrieve the live file")
	}
	sec040CleanupObject(t, live, int64(len(payload)), true)
}

func TestSEC040CleanupLocalBoundaries(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "unsafe", "symlink", "permissions"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			sec040CleanupOpen(t, root)
			payload := []byte("synthetic SEC040 outside data root")
			file := sec040CleanupFile("local-"+mode, "", payload)
			outside := filepath.Join(root, "outside.bin")
			if err := os.WriteFile(outside, payload, 0600); err != nil {
				t.Fatal("SEC040_SETUP could not create isolated boundary sentinel")
			}
			path := filepath.Join(configuration.Get().DataDir, file.SHA1)
			switch mode {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal("SEC040_SETUP could not create synthetic directory")
				}
			case "unsafe":
				file.SHA1 = "../outside.bin"
			case "symlink":
				if err := os.Symlink(outside, path); err != nil {
					if runtime.GOOS == "windows" {
						t.Skip("SEC040_SYMLINK_NOT_RUN Windows host does not allow a temporary symlink")
					}
					t.Fatal("SEC040_SETUP could not create an isolated symlink")
				}
			case "permissions":
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("SEC040_PERMISSIONS_NOT_RUN requires a non-root Unix permission boundary")
				}
				if err := os.WriteFile(path, payload, 0600); err != nil {
					t.Fatal("SEC040_SETUP could not create isolated permission sentinel")
				}
				dataRoot := configuration.Get().DataDir
				t.Cleanup(func() { _ = os.Chmod(dataRoot, 0700) })
				if err := os.Chmod(dataRoot, 0); err != nil {
					t.Fatal("SEC040_SETUP could not restrict the isolated data directory")
				}
			}
			sec040CleanupSave(file)
			sec040CleanupAttempt(t, mode != "missing")
			if mode == "permissions" {
				if err := os.Chmod(configuration.Get().DataDir, 0700); err != nil {
					t.Fatal("SEC040_SETUP could not restore isolated directory permissions")
				}
				if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, payload) {
					t.Fatal("SEC040_PERMISSIONS an inaccessible source was treated as absent")
				}
			}
			if mode == "missing" {
				sec040CleanupRetired(t, file)
			} else {
				sec040CleanupRetained(t, file)
			}
			if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, payload) {
				t.Fatal("SEC040_BOUNDARY cleanup altered a file outside DataDir")
			}
			if mode == "directory" {
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Fatal("SEC040_BOUNDARY cleanup removed a directory as if it were a regular blob")
				}
			}
			if mode == "symlink" {
				if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("SEC040_BOUNDARY cleanup removed an unowned symlink")
				}
			}
		})
	}
}

func sec040CleanupOpen(t *testing.T, root string) {
	t.Helper()
	if root == "" {
		t.Fatal("SEC040_SETUP missing isolated root")
	}
	t.Chdir(root)
	t.Setenv("GOKAPI_CONFIG_DIR", "config")
	t.Setenv("GOKAPI_DATA_DIR", "data")
	t.Setenv("GOKAPI_LOG_STDOUT", "false")
	for _, name := range []string{"config", "data"} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal("SEC040_SETUP could not create isolated directories")
		}
	}
	saved := *configuration.Get()
	cfg := models.Configuration{
		Authentication: models.AuthenticationConfig{SaltAdmin: rand.Text(), SaltFiles: rand.Text()},
		DataDir:        filepath.Join(root, "data"), DatabaseUrl: "sqlite://./state.sqlite",
		ConfigVersion: configupgrade.CurrentConfigVersion, MaxFileSizeMB: 4, MaxMemory: 1,
		ServerUrl: "https://synthetic.invalid/", PublicName: "Synthetic cleanup fixture",
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal("SEC040_SETUP could not encode isolated configuration")
	}
	if err := os.WriteFile(filepath.Join("config", "config.json"), encoded, 0600); err != nil {
		t.Fatal("SEC040_SETUP could not write isolated configuration")
	}
	configuration.Load()
	database.Connect(models.DbConnection{HostUrl: filepath.Join(root, "state.sqlite")})
	t.Cleanup(func() {
		database.Close()
		*configuration.Get() = saved
		if saved.DataDir != "" {
			filesystem.Init(saved.DataDir)
		}
	})
	for _, id := range []int{7002, 7003} {
		database.SaveUser(models.User{Id: id, Name: "synthetic-cleanup-" + strconv.Itoa(id), UserLevel: models.UserLevelUser}, false)
	}
	for _, id := range []int{7002, 7003} {
		if user, found := database.GetUser(id); !found || user.Id != id {
			t.Fatal("SEC040_SETUP distinct positive owners did not persist")
		}
	}
}

func sec040CleanupLogin(t *testing.T, endpoint string) {
	t.Helper()
	if !aws.Init(models.AwsConfig{Bucket: "security-fixture", Region: "us-east-1", Endpoint: endpoint, KeyId: "synthetic", KeySecret: "synthetic"}) {
		t.Fatal("SEC040_SETUP loopback S3 login failed")
	}
	t.Cleanup(aws.LogOut)
}

func sec040CleanupFile(label, bucket string, payload []byte) models.File {
	hash := sha1.Sum(payload) // Existing synthetic blob locator, not a password hash.
	return models.File{
		Id: "sec040-" + label, Name: "synthetic-cleanup.bin", SHA1: hex.EncodeToString(hash[:]),
		HotlinkId: "sec040-" + label + ".png", ContentType: "application/octet-stream",
		AwsBucket: bucket, UserId: 7002, SizeBytes: int64(len(payload)),
		ExpireAt: 1, UnlimitedDownloads: true,
	}
}

func sec040CleanupSave(file models.File) {
	database.SaveMetaData(file)
	database.SaveHotlink(file)
}

func sec040CleanupUpload(t *testing.T, file models.File, payload []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := aws.UploadContext(ctx, bytes.NewReader(payload), file); err != nil {
		t.Fatal("SEC040_SETUP could not upload the synthetic object")
	}
}

func sec040CleanupAttempt(t *testing.T, wantError bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err := storage.CleanUpContext(ctx)
	if wantError && err == nil {
		t.Fatal("SEC040_FAILURE cleanup reported success despite an unconfirmed physical result")
	}
	if !wantError && err != nil {
		t.Fatal("SEC040_RECOVERY cleanup did not finish a confirmed safe retirement")
	}
}

func sec040CleanupRetained(t *testing.T, file models.File) {
	t.Helper()
	stored, found := database.GetMetaDataById(file.Id)
	if !found || stored.UserId != file.UserId || stored.SHA1 != file.SHA1 || stored.AwsBucket != file.AwsBucket {
		t.Fatal("SEC040_RETAIN management record was lost or changed")
	}
	if id, found := database.GetHotlink(file.HotlinkId); !found || id != file.Id {
		t.Fatal("SEC040_RETAIN retryable cleanup lost its hotlink record")
	}
	if _, found := storage.GetFile(file.Id); found {
		t.Fatal("SEC040_ACCESS retained expired metadata became publicly downloadable")
	}
	if _, found := storage.GetFileByHotlink(file.HotlinkId); found {
		t.Fatal("SEC040_ACCESS retained hotlink made an expired file downloadable")
	}
}

func sec040CleanupRetired(t *testing.T, file models.File) {
	t.Helper()
	if _, found := database.GetMetaDataById(file.Id); found {
		t.Fatal("SEC040_RETIRE confirmed cleanup left the metadata record")
	}
	if _, found := database.GetHotlink(file.HotlinkId); found {
		t.Fatal("SEC040_RETIRE confirmed cleanup left the hotlink record")
	}
}

func sec040CleanupObject(t *testing.T, file models.File, size int64, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	exists, length, err := aws.FileExistsContext(ctx, file)
	if err != nil || exists != want || (want && length != size) {
		t.Fatal("SEC040_OBJECT loopback object state did not match the contract")
	}
}
