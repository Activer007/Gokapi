package users

import (
	"errors"
	"sync"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

const minLengthUser = 2

// ErrorNameToShort is returned when the username is too short
var ErrorNameToShort = errors.New("username too short")

// ErrorUserExists is returned when the user already exists
var ErrorUserExists = errors.New("user already exists")

// Serialize this process's name check and initial save for both internal and
// external-auth provisioning. No second write is needed to assign a password.
var createMu sync.Mutex

// Create creates a new user and returns an error if the user already exists or the username is too short
func Create(name string) (models.User, error) {
	return create(name, "")
}

// GenerateTemporaryPassword returns a fresh password for one-time delivery to
// an administrator. It is never persisted directly or attached to models.User.
func GenerateTemporaryPassword() string {
	return helper.GenerateRandomString(max(20, configuration.GetEnvironment().MinLengthPassword+2))
}

// CreateWithTemporaryPassword creates an internal-auth user with a usable
// hashed password and a mandatory first-login password change in the same save.
// On failure no plaintext password is returned.
func CreateWithTemporaryPassword(name string) (models.User, string, error) {
	password := GenerateTemporaryPassword()
	user, err := create(name, password)
	if err != nil {
		return models.User{}, "", err
	}
	return user, password, nil
}

func create(name, temporaryPassword string) (models.User, error) {
	createMu.Lock()
	defer createMu.Unlock()

	if len(name) < minLengthUser {
		return models.User{}, ErrorNameToShort
	}
	_, ok := database.GetUserByName(name)
	if ok {
		return models.User{}, ErrorUserExists
	}
	newUser := models.User{
		Name:      name,
		UserLevel: models.UserLevelUser,
	}
	if temporaryPassword != "" {
		newUser.Password = configuration.HashPassword(temporaryPassword, false, "")
		newUser.ResetPassword = true
	}
	if configuration.GetEnvironment().PermRequestGrantedByDefault {
		newUser.GrantPermission(models.UserPermGuestUploads)
	}
	database.SaveUser(newUser, true)
	newUser, ok = database.GetUserByName(name)
	if !ok {
		return models.User{}, errors.New("user could not be created")
	}
	return newUser, nil
}
