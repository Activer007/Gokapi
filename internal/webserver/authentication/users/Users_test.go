package users

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test"
	"github.com/Activer007/SupaShare/internal/test/testconfiguration"
)

func TestCreate(t *testing.T) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	defer testconfiguration.Delete()

	t.Run("Username too short", func(t *testing.T) {
		_, err := Create("a")
		test.IsEqualBool(t, errors.Is(err, ErrorNameToShort), true)
	})

	t.Run("Successfully create user without default permissions", func(t *testing.T) {
		userName := "testuser1"
		user, err := Create(userName)

		test.IsNil(t, err)
		test.IsEqualString(t, user.Name, userName)
		test.IsEqualInt(t, int(user.UserLevel), int(models.UserLevelUser))

		// Check that guest upload permission was NOT granted
		test.IsEqualBool(t, user.HasPermission(models.UserPermGuestUploads), false)
		test.IsEqualString(t, user.Password, "")
		test.IsEqualBool(t, user.ResetPassword, false)
	})

	t.Run("Duplicate user check", func(t *testing.T) {
		userName := "duplicate"
		_, err := Create(userName)
		test.IsNil(t, err)

		// Try creating the same user again
		_, err = Create(userName)
		test.IsEqualBool(t, errors.Is(err, ErrorUserExists), true)
	})
}

func TestCreateWithTemporaryPassword(t *testing.T) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	t.Cleanup(testconfiguration.Delete)
	// Restore the cached environment after testing a configured longer minimum.
	t.Cleanup(configuration.Load)
	t.Setenv("GOKAPI_MIN_LENGTH_PASSWORD", "32")
	configuration.Load()

	user, password, err := CreateWithTemporaryPassword("FirstLogin")
	if err != nil {
		t.Fatal(err)
	}
	if len(password) < 34 || !user.ResetPassword || user.Password == password || !strings.HasPrefix(user.Password, "argon2id$") {
		t.Fatal("created user must store a hash and require a change of a sufficiently strong temporary password")
	}
	stored, ok := database.GetUser(user.Id)
	if !ok || stored.Password != user.Password || !stored.ResetPassword {
		t.Fatal("initial database record must contain the hash and reset requirement")
	}
	valid, legacy := configuration.VerifyPassword(password, stored.Password, "")
	if !valid || legacy {
		t.Fatal("temporary password must verify against the stored current-format hash")
	}
	if strings.Contains(user.ToJson(), password) || strings.Contains(user.ToJson(), user.Password) || strings.Contains(user.ToJson(), `"password"`) {
		t.Fatal("generic user serialization must not expose any password")
	}
	_, secondPassword, err := CreateWithTemporaryPassword("SecondLogin")
	if err != nil || password == secondPassword {
		t.Fatal("each newly created account must have a fresh password")
	}
	for _, name := range []string{"a", "FIRSTLOGIN"} {
		_, returnedPassword, err := CreateWithTemporaryPassword(name)
		if err == nil || returnedPassword != "" {
			t.Fatal("failed creation must not return credentials")
		}
	}
	unchanged, _ := database.GetUser(user.Id)
	if unchanged.Password != stored.Password {
		t.Fatal("duplicate creation changed the existing account")
	}
}

func TestCreateWithTemporaryPasswordDuplicateRace(t *testing.T) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	defer testconfiguration.Delete()

	var group sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		group.Go(func() {
			_, password, err := CreateWithTemporaryPassword("concurrent-login")
			if err != nil && password != "" {
				results <- errors.New("failed creation exposed credentials")
				return
			}
			results <- err
		})
	}
	group.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrorUserExists) {
			t.Fatalf("unexpected concurrent create failure: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("expected one created account, got %d", created)
	}
}
