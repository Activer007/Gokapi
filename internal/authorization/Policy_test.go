package authorization

import (
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestSEC020PolicyDenyDefault(t *testing.T) {
	user := models.User{Id: 1, UserLevel: models.UserLevelUser}
	for _, action := range []Action{0, 255} {
		if Authorize(user, action, 1) {
			t.Fatal("unknown action accepted")
		}
	}
	for _, action := range []Action{ReadFile, EditFile, ReplaceFile, DeleteFile, ChangeFileOwner, ManageAPIKeys} {
		if Authorize(user, action, 0) {
			t.Fatal("missing resource owner accepted")
		}
	}
	for _, u := range []models.User{{}, {Id: 1, UserLevel: 255, Permissions: models.UserPermissionAll}} {
		if Authorize(u, ManageUsers, 0) || CanGrantAPI(u, models.ApiPermView) {
			t.Fatal("invalid actor accepted")
		}
	}
	if CanGrantAPI(user, models.ApiPermission(1<<15)) {
		t.Fatal("unknown API bit accepted")
	}
	if CanUseAPI(user, models.ApiKey{UserId: 2, Permissions: models.ApiPermView}, models.ApiPermView) {
		t.Fatal("key owner mismatch accepted")
	}
}

func TestSEC020PolicyObjectDelegation(t *testing.T) {
	for _, tc := range []struct {
		action Action
		grant  models.UserPermission
	}{
		{ReadFile, models.UserPermListOtherUploads}, {EditFile, models.UserPermEditOtherUploads},
		{DeleteFile, models.UserPermDeleteOtherUploads}, {ManageAPIKeys, models.UserPermManageApiKeys},
	} {
		u := models.User{Id: 1, UserLevel: models.UserLevelUser}
		if !Authorize(u, tc.action, 1) || Authorize(u, tc.action, 2) {
			t.Fatalf("ownership failure for action %d", tc.action)
		}
		u.GrantPermission(tc.grant)
		if !Authorize(u, tc.action, 2) {
			t.Fatalf("explicit delegation rejected for %d", tc.action)
		}
		u.RemovePermission(tc.grant)
		if Authorize(u, tc.action, 2) {
			t.Fatal("removed delegation retained")
		}
	}
	u := models.User{Id: 1, UserLevel: models.UserLevelAdmin} // rank alone grants no permission
	if Authorize(u, ReplaceFile, 1) || Authorize(u, ManageUsers, 0) || Authorize(u, ChangeFileOwner, 2) {
		t.Fatal("rank bypassed granular permissions")
	}
	u.GrantPermission(models.UserPermReplaceUploads)
	if !Authorize(u, ReplaceFile, 1) || Authorize(u, ReplaceFile, 2) {
		t.Fatal("replace ownership failure")
	}
	u.GrantPermission(models.UserPermReplaceOtherUploads)
	if !Authorize(u, ReplaceFile, 2) {
		t.Fatal("delegated replacement denied")
	}
	u.RemovePermission(models.UserPermReplaceUploads)
	if Authorize(u, ReplaceFile, 2) {
		t.Fatal("cross-owner replacement bypassed base permission")
	}
}

func TestSEC020PolicyKeyIntersection(t *testing.T) {
	for _, tc := range []struct {
		api  models.ApiPermission
		user models.UserPermission
	}{
		{models.ApiPermReplace, models.UserPermReplaceUploads},
		{models.ApiPermManageUsers, models.UserPermManageUsers},
		{models.ApiPermManageLogs, models.UserPermManageLogs},
		{models.ApiPermManageFileRequests, models.UserPermGuestUploads},
	} {
		user := models.User{Id: 1, UserLevel: models.UserLevelUser}
		key := models.ApiKey{UserId: 1, Permissions: tc.api}
		if CanUseAPI(user, key, tc.api) {
			t.Fatal("key exceeded current user")
		}
		user.GrantPermission(tc.user)
		if !CanUseAPI(user, key, tc.api) {
			t.Fatal("valid intersection denied")
		}
		key.RemovePermission(tc.api)
		if CanUseAPI(user, key, tc.api) {
			t.Fatal("user bypassed key permission")
		}
	}
}
