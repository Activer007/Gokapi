// Package authorization contains pure management authorization rules. Callers
// must resolve live credentials/users and resource state before using them.
// Public download capabilities and File Request credentials are distinct paths;
// they must not be converted into a management actor by this package.
package authorization

import "github.com/forceu/gokapi/internal/models"

type Action uint8

const (
	ReadFile Action = iota + 1
	EditFile
	ReplaceFile
	DeleteFile
	ChangeFileOwner
	ManageUsers
	ManageLogs
	ManageFileRequests
	ManageAPIKeys
)

func validUser(user models.User) bool {
	return user.Id > 0 && user.UserLevel <= models.UserLevelUser
}

// Authorize decides the user/object part of an operation. ReadFile also covers
// copying, replacement-source access, and each member of a ZIP. The API gate
// separately checks the key bit. Unknown actions and missing owners deny.
func Authorize(user models.User, action Action, ownerID int) bool {
	if !validUser(user) {
		return false
	}
	switch action {
	case ManageUsers:
		return user.HasPermissionManageUsers()
	case ManageLogs:
		return user.HasPermissionManageLogs()
	case ManageFileRequests:
		return user.HasPermissionCreateFileRequests()
	}
	if ownerID <= 0 {
		return false
	}
	own := user.Id == ownerID
	switch action {
	case ReadFile:
		return own || user.HasPermissionListOtherUploads()
	case EditFile:
		return own || user.HasPermissionEditOtherUploads()
	case ReplaceFile:
		return user.HasPermissionReplace() && (own || user.HasPermissionReplaceOtherUploads())
	case DeleteFile:
		return own || user.HasPermissionDeleteOtherUploads()
	case ChangeFileOwner:
		return user.IsAdmin() && user.HasPermissionEditOtherUploads()
	case ManageAPIKeys:
		return own || user.HasPermissionManageApi()
	default:
		return false
	}
}

const knownAPIPermissions = models.ApiPermView | models.ApiPermUpload | models.ApiPermDelete |
	models.ApiPermApiMod | models.ApiPermEdit | models.ApiPermReplace | models.ApiPermManageUsers |
	models.ApiPermManageLogs | models.ApiPermManageFileRequests | models.ApiPermDownload

// CanGrantAPI bounds both issuance and use of a key by current user permissions.
// Basic file actions and management of one's own keys remain available to users;
// ownership is checked separately. Rank never bypasses the permission bits.
func CanGrantAPI(user models.User, requested models.ApiPermission) bool {
	if !validUser(user) || requested & ^knownAPIPermissions != 0 {
		return false
	}
	for _, requirement := range []struct {
		api  models.ApiPermission
		user models.UserPermission
	}{
		{models.ApiPermReplace, models.UserPermReplaceUploads},
		{models.ApiPermManageUsers, models.UserPermManageUsers},
		{models.ApiPermManageLogs, models.UserPermManageLogs},
		{models.ApiPermManageFileRequests, models.UserPermGuestUploads},
	} {
		if requested&requirement.api != 0 && !user.HasPermission(requirement.user) {
			return false
		}
	}
	return true
}

// CanUseAPI adds the key/user intersection; expiry, revocation and credential
// type are validated by the caller against the current database/route.
func CanUseAPI(user models.User, key models.ApiKey, required models.ApiPermission) bool {
	return key.UserId == user.Id && key.HasPermission(required) && CanGrantAPI(user, required)
}
