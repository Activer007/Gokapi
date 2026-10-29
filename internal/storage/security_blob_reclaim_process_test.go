//go:build test && noaws && security_reclaim

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/configupgrade"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

const (
	sec041ProcessPhase    = "SUPASHARE_SEC041_RECLAIM_PHASE"
	sec041ProcessScene    = "SUPASHARE_SEC041_RECLAIM_SCENE"
	sec041ProcessGuard    = "SUPASHARE_SEC041_RECLAIM_GUARD"
	sec041ProcessCommit   = "SEC041_BLOB_COMMIT_OBSERVED"
	sec041ProcessVerified = "SEC041_BLOB_RESTART_VERIFIED"
	sec041ProcessOwner    = 8411
)

// The producer exits without running database.Close or any deferred cleanup.
// A separate verifier opens that exact scene; neither process reseeds it after
// publication. The package TestMain still creates its own unrelated fixture in
// each child's working directory, outside scene. The parent never switches its
// global database/configuration. This models process exit, not power loss.
func TestSEC041BlobReclaimAfterProcessExit(t *testing.T) {
	switch os.Getenv(sec041ProcessPhase) {
	case "publish":
		sec041ProcessPublish(t)
		return
	case "verify":
		sec041ProcessVerify(t)
		return
	case "":
	default:
		t.Fatal("unexpected reclaim subprocess phase")
	}
	root := t.TempDir()
	scene, guard := filepath.Join(root, "scene"), rand.Text()
	if err := os.Mkdir(scene, 0700); err != nil {
		t.Fatal("could not create the isolated reclaim scene")
	}
	if err := os.WriteFile(filepath.Join(scene, ".sec041-owner"), []byte(guard), 0600); err != nil {
		t.Fatal("could not mark the isolated reclaim scene")
	}
	output, childErr := sec041ProcessRun(t, root, scene, guard, "publish")
	var exit *exec.ExitError
	if !errors.As(childErr, &exit) || exit.ExitCode() != 97 || !bytes.Contains(output, []byte(sec041ProcessCommit)) {
		t.Fatal("producer did not exit 97 after observing a successful replacement commit")
	}
	output, childErr = sec041ProcessRun(t, root, scene, guard, "verify")
	if childErr != nil || !bytes.Contains(output, []byte(sec041ProcessVerified)) {
		t.Fatal("fresh verifier did not prove reclamation, content preservation and idempotence")
	}
}

func sec041ProcessRun(t *testing.T, root, scene, guard, phase string) ([]byte, error) {
	t.Helper()
	work := filepath.Join(root, phase)
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal("could not isolate a reclaim subprocess working directory")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("reclaim subprocess executable unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestSEC041BlobReclaimAfterProcessExit$", "-test.count=1", "-test.v")
	child.Dir = work
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if !strings.HasPrefix(upper, "GOKAPI_") && !strings.HasPrefix(upper, "SUPASHARE_SEC041_RECLAIM_") && upper != "REAL_AWS_CREDENTIALS" {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, sec041ProcessPhase+"="+phase, sec041ProcessScene+"="+scene,
		sec041ProcessGuard+"="+guard, "REAL_AWS_CREDENTIALS=false")
	output, childErr := child.CombinedOutput()
	// Arbitrary TestMain/dependency diagnostics stay in the disposable fixture.
	if err := os.WriteFile(filepath.Join(root, phase+"-private.log"), output, 0600); err != nil {
		t.Fatal("could not retain isolated subprocess diagnostics")
	}
	return output, childErr
}

func sec041ProcessPublish(t *testing.T) {
	sec041ProcessOpen(t, true)
	a, b, oldBytes, newBytes := sec041ProcessFiles()
	database.SaveUser(models.User{Id: sec041ProcessOwner, Name: "synthetic-reclaim-process-owner", UserLevel: models.UserLevelUser}, false)
	for _, item := range []struct {
		file models.File
		data []byte
	}{{a, oldBytes}, {b, newBytes}} {
		if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, item.file.SHA1), item.data, 0600); err != nil {
			t.Fatal("could not write a synthetic reclaim source")
		}
		database.SaveMetaData(item.file)
		if _, found := GetFile(item.file.Id); !found {
			t.Fatal("synthetic live share precondition failed")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, a.Id)
	if err != nil {
		t.Fatal("target metadata gate unavailable")
	}
	updated, err := ReplaceFileContext(ctx, a, b, false)
	unlock()
	if err != nil || updated.Id != a.Id || updated.SHA1 != b.SHA1 {
		t.Fatal("real replacement did not acknowledge its commit")
	}
	// Direct stdout is unbuffered by testing's successful-test log handling.
	// No database Close, t.Cleanup or TestMain cleanup runs after this point.
	if _, err := fmt.Fprintln(os.Stdout, sec041ProcessCommit); err != nil {
		t.Fatal("could not emit the fixed commit stage marker")
	}
	os.Exit(97)
}

func sec041ProcessVerify(t *testing.T) {
	sec041ProcessOpen(t, false)
	a, b, oldBytes, newBytes := sec041ProcessFiles()
	if owner, found := database.GetUser(sec041ProcessOwner); !found || owner.Id != sec041ProcessOwner {
		t.Fatal("the committed scene owner did not survive process exit")
	}
	if len(database.GetAllMetadata()) != 2 {
		t.Fatal("fresh verifier did not open the original two-row scene")
	}
	before := make(map[string]models.File, 2)
	for _, id := range []string{a.Id, b.Id} {
		current, found, err := database.ReadRecoveryFile(t.Context(), id)
		if err != nil || !found || current.UserId != sec041ProcessOwner || current.SHA1 != b.SHA1 {
			t.Fatal("the acknowledged replacement did not survive process exit")
		}
		before[id] = current
	}
	sec041ProcessBytes(t, a.SHA1, oldBytes)
	sec041ProcessBytes(t, b.SHA1, newBytes)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		_, err := CleanUpContext(ctx)
		cancel()
		if err != nil {
			t.Fatal("checked reclamation did not complete after process exit")
		}
		if _, err := os.Lstat(filepath.Join(configuration.Get().DataDir, a.SHA1)); !os.IsNotExist(err) {
			t.Error("old replacement source was not confirmed absent after checked reclamation")
		}
		if len(database.GetAllMetadata()) != 2 {
			t.Fatal("reclamation changed the surviving share count")
		}
		for id, expected := range before {
			current, found, err := database.ReadRecoveryFile(t.Context(), id)
			if err != nil || !found || !reflect.DeepEqual(current, expected) {
				t.Fatal("reclamation or its replay changed surviving metadata")
			}
			if _, found := GetFile(id); !found {
				t.Fatal("reclamation made replacement content unavailable")
			}
		}
		sec041ProcessBytes(t, b.SHA1, newBytes)
	}
	if !t.Failed() {
		t.Log(sec041ProcessVerified)
	}
}

func sec041ProcessOpen(t *testing.T, create bool) {
	t.Helper()
	scene, guard := os.Getenv(sec041ProcessScene), os.Getenv(sec041ProcessGuard)
	if !filepath.IsAbs(scene) || len(guard) < 20 {
		t.Fatal("isolated scene ownership precondition missing")
	}
	marker, err := os.ReadFile(filepath.Join(scene, ".sec041-owner"))
	if err != nil || !bytes.Equal(marker, []byte(guard)) {
		t.Fatal("isolated scene ownership could not be verified")
	}
	// TestMain's fixture is separate. Closing this child's original connection
	// does not touch the parent or add those rows to the scene under test.
	database.Close()
	t.Chdir(scene)
	t.Setenv("GOKAPI_CONFIG_DIR", "config")
	t.Setenv("GOKAPI_CONFIG_FILE", "config.json")
	t.Setenv("GOKAPI_DATA_DIR", filepath.Join(scene, "data"))
	t.Setenv("GOKAPI_LOG_STDOUT", "false")
	if create {
		for _, name := range []string{"config", "data"} {
			if err := os.Mkdir(name, 0700); err != nil {
				t.Fatal("could not create an empty isolated scene")
			}
		}
		cfg := models.Configuration{
			Authentication: models.AuthenticationConfig{SaltAdmin: strings.Repeat("synthetic-admin-", 3), SaltFiles: strings.Repeat("synthetic-files-", 3)},
			DataDir:        filepath.Join(scene, "data"), DatabaseUrl: "sqlite://./state.sqlite",
			ConfigVersion: configupgrade.CurrentConfigVersion, MaxFileSizeMB: 1, MaxMemory: 1,
			ServerUrl: "https://synthetic.invalid/", PublicName: "Synthetic process fixture",
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal("could not encode isolated configuration")
		}
		if err := os.WriteFile(filepath.Join("config", "config.json"), encoded, 0600); err != nil {
			t.Fatal("could not write isolated configuration")
		}
	} else {
		for _, path := range []string{filepath.Join("config", "config.json"), "state.sqlite"} {
			if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
				t.Fatal("persisted scene configuration or SQLite file missing")
			}
		}
	}
	configuration.Load()
	env := configuration.GetEnvironment()
	if !env.IsParsed() || env.ValidateWholeUploadLimits() != nil || env.ValidateRequestUploadLimits() != nil {
		t.Fatal("metadata quota environment was not initialized")
	}
	database.Connect(models.DbConnection{HostUrl: filepath.Join(scene, "state.sqlite")})
	t.Cleanup(database.Close)
}

func sec041ProcessFiles() (models.File, models.File, []byte, []byte) {
	oldBytes, newBytes := bytes.Repeat([]byte("old!"), 16), bytes.Repeat([]byte("new!"), 16)
	file := func(id string, payload []byte) models.File {
		hash := sha1.Sum(payload) // Existing product locator, not a password hash.
		return models.File{Id: id, Name: id + ".bin", SHA1: hex.EncodeToString(hash[:]), UserId: sec041ProcessOwner,
			Size: "64 B", SizeBytes: int64(len(payload)), ContentType: "application/octet-stream", UnlimitedTime: true, UnlimitedDownloads: true}
	}
	return file("reclaim-process-target", oldBytes), file("reclaim-process-source", newBytes), oldBytes, newBytes
}

func sec041ProcessBytes(t *testing.T, locator string, expected []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, locator))
	if err != nil || len(data) != 64 || !bytes.Equal(data, expected) || sha256.Sum256(data) != sha256.Sum256(expected) {
		t.Fatal("owned canonical bytes or SHA256 changed")
	}
}
