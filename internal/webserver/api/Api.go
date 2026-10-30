package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/logging"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filerequest"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/e2emutex"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/downloadPasswordToken"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/users"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload"
	"github.com/Activer007/SupaShare/internal/webserver/ratelimiter"
)

// LengthPublicId is the length of the public ID used for API keys
const LengthPublicId = 35

// LengthApiKey is the length of the private API key used for authentication
const LengthApiKey = 30

// Process parses the request and executes the API call or returns an error message to the sender
func Process(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("cache-control", "no-store")
	requestUrl := parseRequestUrl(r)

	routing, ok := getRouting(requestUrl)
	if !ok {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		sendError(w, http.StatusBadRequest, errorcodes.InvalidUrl, "Invalid request")
		return
	}
	if !routing.NoJsonResponse {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	}
	var user models.User
	var apiKey models.ApiKey
	user, apiKey, ok = isAuthorisedForApi(r, routing)
	if !ok {
		sendError(w, http.StatusUnauthorized, errorcodes.InvalidApiKey, "Unauthorized")
		return
	}
	if routing.AdminOnly && !user.IsAdmin() {
		sendError(w, http.StatusUnauthorized, errorcodes.AdminOnly, "Unauthorized")
		return
	}
	if routing.RequestParser == nil {
		routing.Continue(w, nil, user, apiKey)
		return
	}
	parser := routing.RequestParser.New()
	err := parser.ParseRequest(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, errorcodes.CannotParse, err.Error())
		return
	}
	routing.Continue(w, parser, user, apiKey)
}

func parseRequestUrl(r *http.Request) string {
	return strings.Replace(r.URL.String(), "/api", "", 1)
}

func apiEditFile(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesModify)
	if !ok {
		panic("invalid parameter passed")
	}
	apimutex.Lock(apimutex.TypeMetaData, request.Id)
	defer apimutex.Unlock(apimutex.TypeMetaData, request.Id)

	file, ok := database.GetMetaDataById(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid file ID provided.")
		return
	}
	if !authorization.Authorize(user, authorization.EditFile, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to edit file.")
		return
	}
	if file.IsFileRequest() && request.foundHeaders["allowedDownloads"] &&
		(request.AllowedDownloads < 1 || request.AllowedDownloads > 10) {
		sendError(w, http.StatusBadRequest, errorcodes.InvalidUserInput, "Collection downloads must be between 1 and 10")
		return
	}
	if request.UnlimitedDownloads {
		file.UnlimitedDownloads = true
	} else {
		if request.AllowedDownloads != 0 {
			file.DownloadsRemaining = request.AllowedDownloads
			file.UnlimitedDownloads = false
		}
	}
	if request.UnlimitedExpiry {
		file.UnlimitedTime = true
	} else {
		if request.ExpiryTimestamp != 0 {
			file.ExpireAt = request.ExpiryTimestamp
			file.UnlimitedTime = false
		}
	}

	if !request.KeepPassword {
		file.PasswordHash = configuration.HashPassword(request.Password, false, "")
		downloadPasswordToken.DeleteAllForFile(file.Id)
	}

	if file.HotlinkId != "" && !storage.IsAbleHotlink(file) {
		database.DeleteHotlink(file.HotlinkId)
		file.HotlinkId = ""
	} else if file.HotlinkId == "" && storage.IsAbleHotlink(file) {
		storage.AddHotlink(&file)
	}

	database.SaveMetaData(file)
	logging.LogEdit(file, user)
	outputFileApiInfo(w, file)
}

// generateNewKey generates and saves a new API key
func generateNewKey(defaultPermissions bool, userId int, friendlyName, filerequstId string) models.ApiKey {
	if friendlyName == "" {
		friendlyName = "Unnamed key"
	}
	newKey := models.ApiKey{
		Id:              helper.GenerateRandomString(LengthApiKey),
		PublicId:        helper.GenerateRandomString(LengthPublicId),
		FriendlyName:    friendlyName,
		Permissions:     models.ApiPermDefault,
		IsSystemKey:     false,
		UserId:          userId,
		UploadRequestId: filerequstId,
	}
	if !defaultPermissions {
		newKey.Permissions = models.ApiPermNone
	}
	database.SaveApiKey(newKey)
	return newKey
}

func apiDeleteKey(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramAuthDelete)
	if !ok {
		panic("invalid parameter passed")
	}
	apiKeyOwner, apiKey, ok := isValidKeyForEditing(request.KeyId)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid key ID provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ManageAPIKeys, apiKeyOwner.Id) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to delete this API key")
		return
	}
	database.DeleteApiKey(apiKey.Id)
}

func apiModifyApiKey(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramAuthModify)
	if !ok {
		panic("invalid parameter passed")
	}
	apimutex.Lock(apimutex.TypeApiKey, request.KeyId)
	defer apimutex.Unlock(apimutex.TypeApiKey, request.KeyId)

	apiKeyOwner, apiKey, ok := isValidKeyForEditing(request.KeyId)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid key ID provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ManageAPIKeys, apiKeyOwner.Id) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to delete this API key")
		return
	}

	if !authorization.CanGrantAPI(apiKeyOwner, request.Permission) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "Insufficient user permission for owner to set this API permission")
		return
	}
	if request.GrantPermission && !apiKey.HasPermission(request.Permission) {
		apiKey.GrantPermission(request.Permission)
		database.SaveApiKey(apiKey)
		return
	}
	if !request.GrantPermission && apiKey.HasPermission(request.Permission) {
		apiKey.RemovePermission(request.Permission)
		database.SaveApiKey(apiKey)
	}
}

// isValidKeyForEditing checks if the provided API key is either a public or private ID and returns the user and API
// key model (including the private ID)
func isValidKeyForEditing(apiKey string) (models.User, models.ApiKey, bool) {
	apiKey = publicKeyToApiKey(apiKey)
	user, fullApiKey, ok := isValidApiKey(apiKey, false, models.ApiPermNone)
	if !ok {
		return models.User{}, models.ApiKey{}, false
	}
	return user, fullApiKey, true
}

func isValidUserForEditing(w http.ResponseWriter, userId int) (models.User, bool) {
	user, ok := database.GetUser(userId)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid user id provided.")
		return models.User{}, false
	}
	return user, true
}

func apiCreateApiKey(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramAuthCreate)
	if !ok {
		panic("invalid parameter passed")
	}

	if configuration.GetEnvironment().DisableApiMenu && !user.IsAdmin() {
		sendError(w, http.StatusForbidden, errorcodes.NoPermission, "User API keys are disabled for this instance")
		return
	}

	key := generateNewKey(request.BasicPermissions, user.Id, request.FriendlyName, "")
	output := models.ApiKeyOutput{
		Result:   "OK",
		Id:       key.Id,
		PublicId: key.PublicId,
	}
	result, err := json.Marshal(output)
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiCreateUser(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramUserCreate)
	if !ok {
		panic("invalid parameter passed")
	}
	var newUser models.User
	var password string
	var err error
	if configuration.Get().Authentication.Method == models.AuthenticationInternal {
		newUser, password, err = users.CreateWithTemporaryPassword(request.Username)
	} else {
		newUser, err = users.Create(request.Username)
	}
	if err != nil {
		switch {
		case errors.Is(err, users.ErrorNameToShort):
			sendError(w, http.StatusBadRequest, errorcodes.NoPermission, "Invalid username provided.")
		case errors.Is(err, users.ErrorUserExists):
			sendError(w, http.StatusConflict, errorcodes.AlreadyExists, "User already exists.")
		default:
			sendError(w, http.StatusInternalServerError, errorcodes.InternalServer, err.Error())
		}
		return
	}
	logging.LogUserCreation(newUser, user)
	if password == "" {
		_, _ = w.Write([]byte(newUser.ToJson()))
		return
	}
	response := struct {
		models.User
		TemporaryPassword string `json:"password"`
		LoginURL          string `json:"loginUrl"`
	}{User: newUser, TemporaryPassword: password, LoginURL: credentialLoginURL()}
	setCredentialResponseHeaders(w)
	result, err := json.Marshal(response)
	helper.Check(err)
	_, _ = w.Write(result)
}

// Use the configured external base, including any subpath, rather than an
// untrusted incoming Host or forwarding header when sharing login information.
func credentialLoginURL() string {
	return strings.TrimRight(configuration.Get().ServerUrl, "/") + "/login"
}

func setCredentialResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func apiChangeFriendlyName(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramAuthFriendlyName)
	if !ok {
		panic("invalid parameter passed")
	}

	ownerApiKey, apiKey, ok := isValidKeyForEditing(request.KeyId)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid key ID provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ManageAPIKeys, ownerApiKey.Id) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to edit this API key")
		return
	}
	err := renameApiKeyFriendlyName(apiKey.Id, request.FriendlyName)
	if err != nil {
		sendError(w, http.StatusInternalServerError, errorcodes.InternalServer, err.Error())
		return
	}
}

func renameApiKeyFriendlyName(id string, newName string) error {
	if newName == "" {
		newName = "Unnamed key"
	}

	apimutex.Lock(apimutex.TypeApiKey, id)
	defer apimutex.Unlock(apimutex.TypeApiKey, id)

	key, ok := database.GetApiKey(id)
	if !ok {
		return errors.New("could not modify API key")
	}
	if key.FriendlyName != newName {
		key.FriendlyName = newName
		database.SaveApiKey(key)
	}
	return nil
}

const deletionStatusHeader = "X-SupaShare-Deletion-Status"

func apiDeleteFile(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesDelete)
	if !ok {
		panic("invalid parameter passed")
	}
	ctx := chunkRequestContext(request.Request)
	file, found, err := database.ReadRecoveryFile(ctx, request.Id)
	if err != nil {
		sendFileDeletionError(w, err, false)
		return
	}
	if !found {
		sendFileDeletionError(w, storage.ErrorFileNotFound, false)
		return
	}
	if !authorization.Authorize(user, authorization.DeleteFile, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to delete this file")
		return
	}
	var result storage.DeletionResult
	if request.DelaySeconds == 0 {
		result, err = storage.DeleteFileContext(ctx, file, true)
	} else {
		result, err = storage.ScheduleFileDeletionContext(ctx, file, request.DelaySeconds*1000, true)
	}
	switch result.State {
	case storage.DeletionAccepted, storage.DeletionPending, storage.DeletionDeleted, storage.DeletionShared:
		w.Header().Set(deletionStatusHeader, result.State)
		logging.LogDelete(result.File, user)
	}
	if err != nil {
		if result.State == storage.DeletionPending {
			sendError(w, http.StatusServiceUnavailable, errorcodes.InternalServer, "File cleanup pending; retry later")
			return
		}
		sendFileDeletionError(w, err, false)
	}
}

func apiRestoreFile(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesRestore)
	if !ok {
		panic("invalid parameter passed")
	}
	ctx := chunkRequestContext(request.Request)
	file, found, err := database.ReadRecoveryFile(ctx, request.Id)
	if err != nil {
		sendFileDeletionError(w, err, true)
		return
	}
	if !found {
		sendFileDeletionError(w, storage.ErrorFileNotFound, true)
		return
	}
	if !authorization.Authorize(user, authorization.DeleteFile, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to restore this file")
		return
	}
	file, err = storage.CancelFileDeletionContext(ctx, file)
	if err != nil {
		sendFileDeletionError(w, err, true)
		return
	}
	logging.LogRestore(file, user)
	outputFileJson(w, file)
}

func sendFileDeletionError(w http.ResponseWriter, err error, restore bool) {
	switch {
	case errors.Is(err, storage.ErrorFileNotFound):
		message := "Invalid file ID provided."
		if restore {
			message = "Invalid file ID provided or file has already been deleted."
		}
		sendError(w, http.StatusNotFound, errorcodes.NotFound, message)
	case errors.Is(err, uploadquota.ErrConflict):
		sendError(w, http.StatusConflict, errorcodes.ResourceCanNotBeEdited, "File state changed; retry the request")
	case !restore && errors.Is(err, uploadquota.ErrInvalid):
		sendError(w, http.StatusBadRequest, errorcodes.InvalidUserInput, "Invalid file operation")
	default:
		sendError(w, http.StatusServiceUnavailable, errorcodes.InternalServer, "File operation unavailable")
	}
}

func apiChunkAdd(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramChunkAdd)
	if !ok {
		panic("invalid parameter passed")
	}
	statusCode, errCode, errString := processNewChunk(w, request, configuration.Get().MaxFileSizeMB, "", user.Id)
	if statusCode != http.StatusOK {
		sendError(w, statusCode, errCode, errString)
	}
}

func apiChunkReserve(w http.ResponseWriter, r requestParser, user models.User, apikey models.ApiKey) {
	request, ok := r.(*paramChunkReserve)
	if !ok {
		panic("invalid parameter passed")
	}
	fileRequest, ok, status, code, message := checkFileRequestAndApiKey(request.Id, apikey)
	if !ok {
		sendError(w, status, code, message)
		return
	}
	uuid, err := fileupload.ReserveFileRequestUpload(chunkRequestContext(request.Request), user.Id, fileRequest.Id)
	if err != nil {
		sendUploadAdmissionError(w, err)
		return
	}
	result, err := json.Marshal(struct {
		Result string `json:"Result"`
		Uuid   string `json:"Uuid"`
	}{"OK", uuid})
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiChunkUnreserve(w http.ResponseWriter, r requestParser, user models.User, apikey models.ApiKey) {
	request, ok := r.(*paramChunkUnreserve)
	if !ok {
		panic("invalid parameter passed")
	}
	// Existing upload cleanup requires the current capability and its request
	// scope, but cannot be prevented by the request's count or expiry limit.
	fileRequest, ok, status, code, message := fileRequestForCredential(request.Id, apikey)
	if !ok {
		sendError(w, status, code, message)
		return
	}
	if err := fileupload.CancelFileRequestUpload(chunkRequestContext(request.Request), user.Id, fileRequest.Id, request.Uuid); err != nil {
		sendUploadAdmissionError(w, err)
		return
	}
	_, _ = w.Write([]byte(`{"Result":"OK"}`))
}

func apiChunkUploadRequestAdd(w http.ResponseWriter, r requestParser, user models.User, apikey models.ApiKey) {
	request, ok := r.(*paramChunkUploadRequestAdd)
	if !ok {
		panic("invalid parameter passed")
	}
	fileRequest, ok, status, errorCode, errorMsg := checkFileRequestAndApiKey(request.FileRequestId, apikey)
	if !ok {
		sendError(w, status, errorCode, errorMsg)
		return
	}
	maxUpload := configuration.Get().MaxFileSizeMB
	if !user.IsAdmin() && configuration.GetEnvironment().MaxSizeGuestUploadMb != 0 {
		maxUpload = min(maxUpload, configuration.GetEnvironment().MaxSizeGuestUploadMb)
	}
	if !fileRequest.IsUnlimitedSize() {
		maxUpload = min(maxUpload, fileRequest.MaxSize)
	}
	statusCode, errorCode, errString := processNewChunk(w, request, maxUpload, fileRequest.Id, user.Id)
	if statusCode != http.StatusOK {
		sendError(w, statusCode, errorCode, errString)
	}
}

func fileRequestForCredential(id string, key models.ApiKey) (models.FileRequest, bool, int, int, string) {
	request, ok := filerequest.Get(id)
	if !ok {
		return models.FileRequest{}, false, http.StatusNotFound, errorcodes.NotFound, "FileRequest does not exist with the given ID"
	}
	if request.ApiKey != key.Id {
		return models.FileRequest{}, false, http.StatusUnauthorized, errorcodes.InvalidApiKey, "Invalid API key"
	}
	return request, true, 0, 0, ""
}

func checkFileRequestAndApiKey(id string, key models.ApiKey) (models.FileRequest, bool, int, int, string) {
	request, ok, status, code, message := fileRequestForCredential(id, key)
	if !ok {
		return request, ok, status, code, message
	}
	if !request.IsUnlimitedTime() && request.Expiry < time.Now().Unix() {
		return models.FileRequest{}, false, http.StatusUnauthorized, errorcodes.RequestExpired, "Filerequest has expired"
	}
	if !request.IsUnlimitedFiles() && request.UploadedFiles >= request.MaxFiles {
		return models.FileRequest{}, false, http.StatusUnauthorized, errorcodes.CannotUploadMoreFiles, "Max file count has already been reached for this file request"
	}
	return request, true, 0, 0, ""
}

type chunkParams interface {
	GetRequest() *http.Request
}

func processNewChunk(w http.ResponseWriter, request chunkParams, maxFileSizeMb int, filerequestId string, userId int) (int, int, string) {
	maxUpload, err := fileupload.MaxUploadBytes(maxFileSizeMb)
	if err != nil {
		return http.StatusBadRequest, errorcodes.FileTooLarge, err.Error()
	}
	errCode, err := fileupload.ProcessNewChunk(w, request.GetRequest(), true, filerequestId, userId, maxUpload)
	if err != nil {
		status := fileupload.ReceiveErrorStatus(err)
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		return status, errCode, err.Error()
	}
	return http.StatusOK, 0, ""
}

func apiChunkComplete(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramChunkComplete)
	if !ok {
		panic("invalid parameter passed")
	}
	params := fileupload.CreateUploadConfig(request.AllowedDownloads, request.ExpiryDays, request.Password,
		request.UnlimitedTime, request.UnlimitedDownloads, request.IsE2E, request.RealSize, "")
	task, err := fileupload.PrepareChunkCompletion(chunkRequestContext(request.Request), request.Uuid, request.FileHeader, user.Id, params)
	if err != nil {
		sendUploadAdmissionError(w, err)
		return
	}
	dispatchChunkCompletion(w, task, user, params, request.IsNonBlocking)
}

func chunkRequestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	} // direct internal/test calls
	return request.Context()
}

func sendUploadAdmissionError(w http.ResponseWriter, err error) {
	if w == nil {
		return
	}
	status, code, message := fileupload.WholeUploadErrorResponse(err)
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	sendError(w, status, code, message)
}

func dispatchChunkCompletion(w http.ResponseWriter, task *fileupload.PreparedCompletion, user models.User, params models.UploadParameters, asynchronous bool) {
	if asynchronous {
		if task.AlreadyCommitted() {
			defer task.Done()
			if _, err := task.Run(); err != nil {
				sendUploadAdmissionError(w, err)
				return
			}
		} else {
			// Prepare has frozen the upload and durably claimed its fixed FileID.
			// Distinct workers are bounded by durable active/staging admission.
			go completePreparedChunk(nil, task, user, params, true)
		}
		_, _ = io.WriteString(w, "{\"result\":\"OK\"}")
		return
	}
	completePreparedChunk(w, task, user, params, false)
}

func completePreparedChunk(w http.ResponseWriter, task *fileupload.PreparedCompletion, user models.User, params models.UploadParameters, detached bool) {
	// Run finishes storage work before returning. Logging still reads the
	// database, so service shutdown must wait through the entire worker tail.
	defer task.Done()
	var file models.File
	var err error
	if detached {
		file, err = task.RunDetached()
	} else {
		file, err = task.Run()
	}
	if err != nil {
		sendUploadAdmissionError(w, err)
		return
	}
	if !task.AlreadyCommitted() {
		request, _ := filerequest.Get(params.FileRequestId)
		logging.LogUpload(file, user, request)
	}
	outputFileJson(w, file)
}

func apiChunkUploadRequestComplete(w http.ResponseWriter, r requestParser, user models.User, apikey models.ApiKey) {
	request, ok := r.(*paramChunkUploadRequestComplete)
	if !ok {
		panic("invalid parameter passed")
	}
	// Capacity is checked atomically against the current request in Prepare and
	// Commit. A committed receipt can be replayed even when the count is full.
	fileRequest, ok, status, code, message := fileRequestForCredential(request.FileRequestId, apikey)
	if !ok {
		sendError(w, status, code, message)
		return
	}
	if fileRequest.IsExpired() {
		sendError(w, http.StatusUnauthorized, errorcodes.RequestExpired, "Filerequest has expired")
		return
	}
	params := fileupload.CreateUploadConfig(fileRequest.AllowedDownloads, 0, "", true, fileRequest.AllowedDownloads == 0, false, request.FileSize, fileRequest.Id)
	task, err := fileupload.PrepareChunkCompletion(chunkRequestContext(request.Request), request.Uuid, request.FileHeader, user.Id, params)
	if err != nil {
		sendUploadAdmissionError(w, err)
		return
	}
	dispatchChunkCompletion(w, task, user, params, request.IsNonBlocking)
}

func apiVersionInfo(w http.ResponseWriter, _ requestParser, _ models.User, _ models.ApiKey) {
	type versionInfo struct {
		Version    string
		VersionInt int
	}
	result, err := json.Marshal(versionInfo{versionReadable, versionInt})
	helper.Check(err)
	_, _ = w.Write(result)
}
func apiConfigInfo(w http.ResponseWriter, _ requestParser, _ models.User, _ models.ApiKey) {
	type configInfo struct {
		MaxFilesize               int
		MaxChunksize              int
		EndToEndEncryptionEnabled bool
	}
	config := configuration.Get()
	result, err := json.Marshal(configInfo{
		MaxFilesize:               config.MaxFileSizeMB,
		MaxChunksize:              config.ChunkSize,
		EndToEndEncryptionEnabled: config.Encryption.Level == encryption.EndToEndEncryption,
	})
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiList(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesListAll)
	if !ok {
		panic("invalid parameter passed")
	}
	validFiles := getFilesForUser(user, request.ShowFileRequests, false)
	result, err := json.Marshal(validFiles)
	helper.Check(err)
	_, _ = w.Write(result)
}

func getFilesForUser(user models.User, includeUploadRequests, onlyEndToEndEncryptedFiles bool) []models.FileApiOutput {
	var validFiles []models.FileApiOutput
	timeNow := time.Now().Unix()
	config := configuration.Get()
	for _, element := range database.GetAllMetadata() {
		if !includeUploadRequests && element.IsFileRequest() {
			continue
		}
		if onlyEndToEndEncryptedFiles && !element.Encryption.IsEndToEndEncrypted {
			continue
		}
		if authorization.Authorize(user, authorization.ReadFile, element.UserId) {
			if !storage.IsExpiredFile(element, timeNow) {
				file, err := element.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
				helper.Check(err)
				validFiles = append(validFiles, file)
			}
		}
	}
	return validFiles
}

func apiListSingle(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesListSingle)
	if !ok {
		panic("invalid parameter passed")
	}
	file, ok := storage.GetFile(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "File not found")
		return
	}
	if !authorization.Authorize(user, authorization.ReadFile, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to view file")
		return
	}
	config := configuration.Get()
	output, err := file.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
	helper.Check(err)
	result, err := json.Marshal(output)
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiDownloadSingle(w http.ResponseWriter, r requestParser, user models.User, apiKey models.ApiKey) {
	request, ok := r.(*paramFilesDownloadSingle)
	if !ok {
		panic("invalid parameter passed")
	}
	file, statusCode, errCode, errMessage := checkDownloadAllowed(request.Id, user)
	if statusCode != 0 {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		sendError(w, statusCode, errCode, errMessage)
		return
	}
	if !request.PresignUrl {
		forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
		storage.ServeFile(file, w, request.WebRequest, true, request.IncreaseCounter, forceDecryption, false)
		return
	}
	createAndOutputPresignedUrl([]string{file.Id}, w, "", apiKey)
}

func apiDownloadZip(w http.ResponseWriter, r requestParser, user models.User, apiKey models.ApiKey) {
	request, ok := r.(*paramFilesDownloadZip)
	if !ok {
		panic("invalid parameter passed")
	}
	requestedFiles := make([]models.File, 0)
	requestedFileIds := make([]string, 0)
	for _, fileId := range request.Ids {
		file, statusCode, errCode, errMessage := checkDownloadAllowed(fileId, user)
		if statusCode != 0 {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			sendError(w, statusCode, errCode, errMessage)
			return
		}
		requestedFiles = append(requestedFiles, file)
		requestedFileIds = append(requestedFileIds, file.Id)
	}
	if !request.PresignUrl {
		storage.ServeFilesAsZip(requestedFiles, request.Filename, w, request.WebRequest)
		return
	}
	createAndOutputPresignedUrl(requestedFileIds, w, request.Filename, apiKey)
}

func checkDownloadAllowed(fileId string, user models.User) (models.File, int, int, string) {
	file, ok := storage.GetFile(fileId)
	if !ok {
		return models.File{}, http.StatusNotFound, errorcodes.NotFound, "file not found"
	}
	if !authorization.Authorize(user, authorization.ReadFile, file.UserId) {
		return models.File{}, http.StatusUnauthorized, errorcodes.NoPermission, "no permission to download file"
	}
	return file, 0, 0, ""
}

func createAndOutputPresignedUrl(ids []string, w http.ResponseWriter, filename string, apiKey models.ApiKey) {
	presignUrl := models.Presign{
		Id:       helper.GenerateRandomString(60),
		FileIds:  ids,
		Expiry:   time.Now().Add(time.Second * 30).Unix(),
		Filename: filename,
		UserId:   apiKey.UserId,
		ApiKeyId: apiKey.Id,
	}
	presign.Save(presignUrl)
	response := struct {
		Result      string `json:"Result"`
		DownloadUrl string `json:"downloadUrl"`
	}{"OK", configuration.Get().ServerUrl + "downloadPresigned?key=" + presignUrl.Id}
	result, err := json.Marshal(response)
	helper.Check(err)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	_, _ = w.Write(result)
}

// GetDownloadUser resolves current management download authority without
// changing usage telemetry. File Request credentials cannot grant it.
func GetDownloadUser(keyID string) (models.User, bool) {
	user, key, ok := isValidApiKey(keyID, false, models.ApiPermDownload)
	return user, ok && !key.IsUploadRequestKey()
}

func apiUploadFile(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesAdd)
	if !ok {
		panic("invalid parameter passed")
	}
	err := fileupload.ProcessCompleteFile(w, request.Request, user.Id, configuration.Get().MaxMemory)
	if err != nil {
		status, code, message := fileupload.WholeUploadErrorResponse(err)
		if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		sendError(w, status, code, message)
		return
	}
}

func apiDuplicateFile(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesDuplicate)
	if !ok {
		panic("invalid parameter passed")
	}
	file, ok := storage.GetFile(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid id provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ReadFile, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to duplicate this file")
		return
	}
	if file.IsFileRequest() && request.foundHeaders["allowedDownloads"] &&
		(request.AllowedDownloads < 1 || request.AllowedDownloads > 10) {
		sendError(w, http.StatusBadRequest, errorcodes.InvalidUserInput, "Collection downloads must be between 1 and 10")
		return
	}
	uploadConfig := fileupload.CreateUploadConfig(request.AllowedDownloads,
		request.ExpiryDays,
		request.Password,
		request.UnlimitedTime,
		request.UnlimitedDownloads,
		false, // is not being used by storage.DuplicateFile
		0,     // is not being used by storage.DuplicateFile
		"")
	uploadConfig.UserId = user.Id
	newFile, err := storage.DuplicateFileContext(chunkRequestContext(request.Request), file, request.RequestedChanges, request.FileName, uploadConfig)
	if err != nil {
		sendUploadAdmissionError(w, fileupload.UploadAdmissionError(err))
		return
	}
	outputFileApiInfo(w, newFile)
}

func apiChangeFileOwner(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramFilesChangeOwner)
	if !ok {
		panic("invalid parameter passed")
	}

	apimutex.Lock(apimutex.TypeMetaData, request.Id)
	defer apimutex.Unlock(apimutex.TypeMetaData, request.Id)

	file, ok := storage.GetFile(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid id provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ChangeFileOwner, file.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to edit this file")
		return
	}
	_, exists := database.GetUser(request.NewOwner)
	if !exists {
		sendError(w, http.StatusBadRequest, errorcodes.NotFound, "User does not exist")
		return
	}
	file, err := storage.TransferFileOwnerContext(chunkRequestContext(request.Request), file, request.NewOwner)
	if err != nil {
		sendUploadAdmissionError(w, fileupload.UploadAdmissionError(err))
		return
	}
	outputFileApiInfo(w, file)
}

func apiReplaceFile(w http.ResponseWriter, r requestParser, user models.User, apikey models.ApiKey) {
	request, ok := r.(*paramFilesReplace)
	if !ok {
		panic("invalid parameter passed")
	}
	keys := []string{request.Id}
	if request.DeleteNewFile {
		keys = append(keys, request.IdNewContent)
	}
	unlock := apimutex.LockMany(apimutex.TypeMetaData, keys...)
	defer unlock()
	fileOriginal, ok := storage.GetFile(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid id provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ReplaceFile, fileOriginal.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to replace this file")
		return
	}

	if fileOriginal.IsFileRequest() {
		sendError(w, http.StatusBadRequest, errorcodes.UnsupportedFile, "Cannot replace a file request upload")
		return
	}
	fileNewContent, ok := storage.GetFile(request.IdNewContent)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Invalid id provided.")
		return
	}
	if !authorization.Authorize(user, authorization.ReadFile, fileNewContent.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to duplicate this file")
		return
	}

	if request.DeleteNewFile &&
		(!apikey.HasPermissionDelete() ||
			!authorization.Authorize(user, authorization.DeleteFile, fileNewContent.UserId)) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to delete original file")
		return
	}

	modifiedFile, err := storage.ReplaceFileContext(chunkRequestContext(request.Request), fileOriginal, fileNewContent, request.DeleteNewFile)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrorReplaceE2EFile):
			sendError(w, http.StatusBadRequest, errorcodes.EndToEndNotSupported, "End-to-End encrypted files cannot be replaced")
		case errors.Is(err, storage.ErrorFileNotFound):
			sendError(w, http.StatusNotFound, errorcodes.NotFound, "A file with such an ID could not be found")
		case errors.Is(err, storage.ErrorReplaceRequestFile):
			sendError(w, http.StatusBadRequest, errorcodes.UnsupportedFile, "Cannot replace a file request upload")
		default:
			sendUploadAdmissionError(w, fileupload.UploadAdmissionError(err))
		}
		return
	}
	logging.LogReplace(fileOriginal, modifiedFile, user)
	outputFileApiInfo(w, modifiedFile)
}

func outputFileApiInfo(w http.ResponseWriter, file models.File) {
	config := configuration.Get()
	publicOutput, err := file.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
	helper.Check(err)
	result, err := json.Marshal(publicOutput)
	helper.Check(err)
	_, _ = w.Write(result)
}

func outputFileJson(w http.ResponseWriter, file models.File) {
	if w == nil {
		return
	}
	config := configuration.Get()
	_, _ = io.WriteString(w, file.ToJsonResult(config.ServerUrl, config.IncludeFilename))
}

func apiModifyUser(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramUserModify)
	if !ok {
		panic("invalid parameter passed")
	}
	idStr := strconv.Itoa(request.Id)
	apimutex.Lock(apimutex.TypeUser, idStr)
	defer apimutex.Unlock(apimutex.TypeUser, idStr)

	userEdit, ok := isValidUserForEditing(w, request.Id)
	if !ok {
		return
	}
	if userEdit.IsSuperAdmin() {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot modify super admin")
		return
	}
	if userEdit.IsSameUser(user.Id) {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot modify yourself")
		return
	}
	if request.GrantPermission && !user.HasPermission(request.Permission) {
		sendError(w, http.StatusBadRequest, errorcodes.NoPermission, "Cannot grant rights the user does not have")
		return
	}
	logging.LogUserEdit(userEdit, user)
	if request.GrantPermission {
		if !userEdit.HasPermission(request.Permission) {
			userEdit.GrantPermission(request.Permission)
			database.SaveUser(userEdit, false)
		}
		return
	}
	if userEdit.HasPermission(request.Permission) {
		userEdit.RemovePermission(request.Permission)
		database.SaveUser(userEdit, false)
		updateApiKeyPermsOnUserPermChange(userEdit.Id, request.Permission)
	}
}

func apiChangeUserRank(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramUserChangeRank)
	if !ok {
		panic("invalid parameter passed")
	}
	idStr := strconv.Itoa(request.Id)
	apimutex.Lock(apimutex.TypeUser, idStr)
	defer apimutex.Unlock(apimutex.TypeUser, idStr)

	userEdit, ok := isValidUserForEditing(w, request.Id)
	if !ok {
		return
	}
	if userEdit.IsSameUser(user.Id) {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot modify yourself")
		return
	}
	if userEdit.IsSuperAdmin() {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot modify super admin")
		return
	}
	if request.NewRank == models.UserLevelAdmin && !user.IsAdmin() {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Only admins can promote users to admin")
		return
	}

	userEdit.UserLevel = request.NewRank
	switch request.NewRank {
	case models.UserLevelAdmin:
		userEdit.Permissions = models.UserPermissionAll
	case models.UserLevelUser:
		userEdit.Permissions = models.UserPermissionNone
		updateApiKeyPermsOnUserPermChange(userEdit.Id, models.UserPermReplaceUploads)
		updateApiKeyPermsOnUserPermChange(userEdit.Id, models.UserPermManageUsers)
		updateApiKeyPermsOnUserPermChange(userEdit.Id, models.UserPermManageLogs)
		updateApiKeyPermsOnUserPermChange(userEdit.Id, models.UserPermGuestUploads)
	default:
		sendError(w, http.StatusBadRequest, errorcodes.InvalidUserInput, "invalid rank sent")
		return
	}
	logging.LogUserEdit(userEdit, user)
	database.SaveUser(userEdit, false)
}

func updateApiKeyPermsOnUserPermChange(userId int, userPerm models.UserPermission) {
	var affectedPermission models.ApiPermission
	switch userPerm {
	case models.UserPermManageUsers:
		affectedPermission = models.ApiPermManageUsers
	case models.UserPermReplaceUploads:
		affectedPermission = models.ApiPermReplace
	case models.UserPermManageLogs:
		affectedPermission = models.ApiPermManageLogs
	case models.UserPermGuestUploads:
		affectedPermission = models.ApiPermManageFileRequests
	default:
		return
	}
	for _, apiKey := range database.GetAllApiKeys() {
		if apiKey.UserId != userId {
			continue
		}
		if apiKey.HasPermission(affectedPermission) {
			apiKey.RemovePermission(affectedPermission)
			database.SaveApiKey(apiKey)
		}
	}
}

func apiResetPassword(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramUserResetPw)
	if !ok {
		panic("invalid parameter passed")
	}
	userToEdit, ok := isValidUserForEditing(w, request.Id)
	if !ok {
		return
	}
	if userToEdit.IsSuperAdmin() {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot reset password of super admin")
		return
	}
	if userToEdit.IsSameUser(user.Id) {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot reset password of yourself")
		return
	}
	userToEdit.ResetPassword = true
	password := ""
	if request.NewPassword {
		password = users.GenerateTemporaryPassword()
		userToEdit.Password = configuration.HashPassword(password, false, "")
	}
	database.DeleteAllSessionsByUser(userToEdit.Id)
	database.SaveUser(userToEdit, false)
	resultStruct := struct {
		Result      string `json:"Result"`
		NewPassword string `json:"password"`
		Username    string `json:"username,omitempty"`
		LoginURL    string `json:"loginUrl,omitempty"`
	}{Result: "OK", NewPassword: password}
	if request.NewPassword {
		resultStruct.Username = userToEdit.Name
		resultStruct.LoginURL = credentialLoginURL()
		setCredentialResponseHeaders(w)
	}
	result, _ := json.Marshal(resultStruct)
	_, _ = w.Write(result)
}

func apiDeleteUser(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramUserDelete)
	if !ok {
		panic("invalid parameter passed")
	}
	userToDelete, ok := isValidUserForEditing(w, request.Id)
	if !ok {
		return
	}
	if userToDelete.IsSuperAdmin() {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot delete super admin")
		return
	}
	if userToDelete.IsSameUser(user.Id) {
		sendError(w, http.StatusBadRequest, errorcodes.ResourceCanNotBeEdited, "Cannot delete yourself")
		return
	}
	ctx, cancel := context.WithTimeout(chunkRequestContext(request.Request), 30*time.Second)
	defer cancel()
	database.DeleteAllSessionsByUser(userToDelete.Id)

	for _, apiKey := range database.GetAllApiKeys() {
		if apiKey.UserId == userToDelete.Id {
			database.DeleteApiKey(apiKey.Id)
		}
	}

	if err := deleteUserFiles(ctx, userToDelete.Id, user.Id, request.DeleteFiles); err != nil {
		w.Header().Set(deletionStatusHeader, storage.DeletionPending)
		if errors.Is(err, uploadquota.ErrConflict) {
			sendFileDeletionError(w, err, false)
		} else {
			sendError(w, http.StatusServiceUnavailable, errorcodes.InternalServer, "User file operation pending; retry later")
		}
		return
	}
	logging.LogUserDeletion(userToDelete, user)

	database.DeleteEnd2EndInfo(userToDelete.Id)

	for _, fRequest := range database.GetAllFileRequests() {
		if fRequest.UserId == userToDelete.Id {
			if request.DeleteFiles {
				if err := filerequest.DeleteContext(ctx, fRequest); err != nil {
					w.Header().Set(deletionStatusHeader, storage.DeletionPending)
					sendFileDeletionError(w, err, false)
					return
				}
			} else {
				if err := func() error {
					unlock, err := apimutex.LockContext(ctx, apimutex.TypeFileRequest, fRequest.Id)
					if err != nil {
						return err
					}
					defer unlock()
					current, found := database.GetFileRequest(fRequest.Id)
					if !found {
						return nil
					}
					if current.UserId != userToDelete.Id {
						return uploadquota.ErrConflict
					}
					current.UserId = user.Id
					database.SaveFileRequest(current)
					return nil
				}(); err != nil {
					w.Header().Set(deletionStatusHeader, storage.DeletionPending)
					sendFileDeletionError(w, err, false)
					return
				}
			}
		}
	}

	database.DeleteUser(userToDelete.Id)
}

func apiLogsDelete(_ http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramLogsDelete)
	if !ok {
		panic("invalid parameter passed")
	}
	logging.DeleteLogs(user.Name, user.Id, request.Timestamp, request.Request)
}
func apiLogsGet(w http.ResponseWriter, r requestParser, _ models.User, _ models.ApiKey) {
	request, ok := r.(*paramLogsGet)
	if !ok {
		panic("invalid parameter passed")
	}
	result := struct {
		LogEntries string `json:"logEntries"`
		Timestamp  int64  `json:"timestamp"`
	}{}
	if request.Timestamp == 0 {
		result.LogEntries, _ = logging.GetAll()
	} else {
		result.LogEntries = logging.GetSince(request.Timestamp)
	}
	result.Timestamp = time.Now().Unix()
	resultJson, err := json.Marshal(result)
	helper.Check(err)
	_, _ = w.Write(resultJson)
}

func apiLogSystemStatus(w http.ResponseWriter, _ requestParser, _ models.User, _ models.ApiKey) {
	result := struct {
		Uptime                int64  `json:"uptime"`
		TrafficRecordingSince int64  `json:"trafficRecordingSince"`
		CpuLoad               int    `json:"cpuLoad"`
		MemoryUsagePercentage int    `json:"memoryUsagePercentage"`
		DiskUsagePercentage   int    `json:"diskUsagePercentage"`
		ActiveFiles           int    `json:"activeFiles"`
		MemoryUsed            uint64 `json:"memoryUsed"`
		MemoryTotal           uint64 `json:"memoryTotal"`
		DiskUsed              uint64 `json:"diskUsed"`
		DiskTotal             uint64 `json:"diskTotal"`
		DataServed            uint64 `json:"dataServed"`
	}{
		Uptime:      serverstats.GetUptime(),
		CpuLoad:     serverstats.GetCpuUsage(),
		ActiveFiles: serverstats.GetTotalFiles(),
	}
	result.DataServed, result.TrafficRecordingSince = serverstats.GetCurrentTraffic()
	_, result.MemoryUsed, result.MemoryTotal, result.MemoryUsagePercentage = serverstats.GetMemoryInfo()
	_, result.DiskUsed, result.DiskTotal, result.DiskUsagePercentage = serverstats.GetDiskInfo()
	resultJson, err := json.Marshal(result)
	if err != nil {
		fmt.Println(err)
		sendError(w, http.StatusInternalServerError, errorcodes.InternalServer, err.Error())
		return
	}
	_, _ = w.Write(resultJson)
}

func apiLogResetTraffic(w http.ResponseWriter, _ requestParser, _ models.User, _ models.ApiKey) {
	serverstats.ClearTraffic()
	_, _ = w.Write([]byte(`{"Result":"OK"}`))
}

func apiE2eGet(w http.ResponseWriter, _ requestParser, user models.User, _ models.ApiKey) {
	if !e2emutex.IsLocked(user.Id) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"result\":\"error\",\"errormessage\":\"mutex was not acquired or has expired\"}"))
		return
	}
	info := database.GetEnd2EndInfo(user.Id)
	// If e2e is supported for upload requests at some point, this needs to be changed
	files := getFilesForUser(user, false, true)
	ids := make([]string, len(files))
	for i, file := range files {
		ids[i] = file.Id
	}
	info.AvailableFiles = ids
	bytesE2e, err := json.Marshal(info)
	helper.Check(err)
	_, _ = w.Write(bytesE2e)
}

func apiE2eSet(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	if !e2emutex.IsLocked(user.Id) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"result\":\"error\",\"errormessage\":\"mutex was not acquired or has expired\"}"))
		return
	}
	request, ok := r.(*paramE2eStore)
	if !ok {
		panic("invalid parameter passed")
	}
	database.SaveEnd2EndInfo(request.EncryptedInfo, user.Id)
	_, _ = w.Write([]byte("{\"result\":\"OK\"}"))
}
func apiE2eMutexLock(w http.ResponseWriter, _ requestParser, user models.User, _ models.ApiKey) {
	e2emutex.Lock(user.Id)
	_, _ = w.Write([]byte("{\"result\":\"OK\"}"))
}

func apiE2eMutexUnlock(w http.ResponseWriter, _ requestParser, user models.User, _ models.ApiKey) {
	e2emutex.Unlock(user.Id)
	_, _ = w.Write([]byte("{\"result\":\"OK\"}"))
}
func apiURequestDelete(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramURequestDelete)
	if !ok {
		panic("invalid parameter passed")
	}

	uploadRequest, ok := database.GetFileRequest(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "FileRequest does not exist with the given ID")
		return
	}
	if !authorization.Authorize(user, authorization.DeleteFile, uploadRequest.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to delete this upload request")
		return
	}
	if err := filerequest.DeleteContext(chunkRequestContext(request.Request), uploadRequest); err != nil {
		w.Header().Set("X-SupaShare-Deletion-Status", storage.DeletionPending)
		sendError(w, http.StatusServiceUnavailable, errorcodes.InternalServer, "File request deletion pending; retry later")
		return
	}
	logging.LogDeleteFileRequest(uploadRequest, user)
	_, _ = w.Write([]byte("{\"result\":\"OK\"}"))
}

func isUserAllowedUnlimited(request *paramURequestSave, isNewRequest bool, user models.User) bool {
	if user.IsAdmin() {
		return true
	}
	isServerLimitMaxSize := configuration.GetEnvironment().MaxSizeGuestUploadMb != 0
	isServerLimitMaxFiles := configuration.GetEnvironment().MaxFilesGuestUpload != 0
	if isServerLimitMaxSize {
		if (request.IsMaxSizeSet || isNewRequest) &&
			(request.MaxSizeMb == 0 || request.MaxSizeMb > configuration.GetEnvironment().MaxSizeGuestUploadMb) {
			return false
		}
	}
	if isServerLimitMaxFiles {
		if (request.IsMaxFilesSet || isNewRequest) &&
			(request.MaxFiles == 0 || request.MaxFiles > configuration.GetEnvironment().MaxFilesGuestUpload) {
			return false
		}
	}

	return true
}

func apiURequestSave(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramURequestSave)
	if !ok {
		panic("invalid parameter passed")
	}
	uploadRequest := models.FileRequest{}
	isNewRequest := request.Id == ""

	if !isUserAllowedUnlimited(request, isNewRequest, user) {
		sendError(w, http.StatusBadRequest, errorcodes.AdminOnly, "Only admin users can create requests with unlimited size / file count"+
			" or values larger than the server's max size / file count")
		return
	}

	if isNewRequest {
		uploadRequest = filerequest.New(user)
	}
	requestID := request.Id
	if isNewRequest {
		requestID = uploadRequest.Id
	}
	ctx, cancel := context.WithTimeout(chunkRequestContext(request.Request), 5*time.Second)
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeFileRequest, requestID)
	if err != nil {
		sendError(w, http.StatusServiceUnavailable, errorcodes.InternalServer, "File request operation unavailable")
		return
	}
	defer unlock()

	if !isNewRequest {
		uploadRequest, ok = database.GetFileRequest(request.Id)
		if !ok {
			sendError(w, http.StatusNotFound, errorcodes.NotFound, "FileRequest does not exist with the given ID")
			return
		}
		if !authorization.Authorize(user, authorization.EditFile, uploadRequest.UserId) {
			sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to edit this upload request")
			return
		}
	} else {
		if _, exists := database.GetFileRequest(uploadRequest.Id); exists {
			sendFileDeletionError(w, uploadquota.ErrConflict, false)
			return
		}
		apiKey := generateNewKey(false, user.Id, "File Request Public Access", uploadRequest.Id)
		uploadRequest.ApiKey = apiKey.Id
	}

	if request.Name == "" {
		if request.IsNameSet || uploadRequest.Name == "" {
			uploadRequest.Name = "Unnamed Request"
		}
	} else {
		uploadRequest.Name = request.Name
	}
	if request.IsExpirySet {
		uploadRequest.Expiry = request.Expiry
	}
	if request.IsAllowedDownloadsSet {
		uploadRequest.AllowedDownloads = request.AllowedDownloads
	}
	if request.IsMaxFilesSet {
		uploadRequest.MaxFiles = request.MaxFiles
	}
	if request.IsMaxSizeSet {
		uploadRequest.MaxSize = request.MaxSizeMb
	}
	if request.IsNotesSet {
		uploadRequest.Notes = request.Notes
	}
	database.SaveFileRequest(uploadRequest)
	uploadRequest, ok = filerequest.Get(uploadRequest.Id)
	if isNewRequest {
		logging.LogCreateFileRequest(uploadRequest, user)
	} else {
		logging.LogEditFileRequest(uploadRequest, user)
	}
	result, err := json.Marshal(uploadRequest)
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiUploadRequestList(w http.ResponseWriter, _ requestParser, user models.User, _ models.ApiKey) {
	userRequests := make([]models.FileRequest, 0)
	for _, request := range filerequest.GetAll() {
		if authorization.Authorize(user, authorization.ReadFile, request.UserId) {
			userRequests = append(userRequests, request)
		}
	}
	result, err := json.Marshal(userRequests)
	helper.Check(err)
	_, _ = w.Write(result)
}

func apiUploadRequestListSingle(w http.ResponseWriter, r requestParser, user models.User, _ models.ApiKey) {
	request, ok := r.(*paramURequestListSingle)
	if !ok {
		panic("invalid parameter passed")
	}

	uploadRequest, ok := filerequest.Get(request.Id)
	if !ok {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "FileRequest does not exist with the given ID")
		return
	}
	if !authorization.Authorize(user, authorization.ReadFile, uploadRequest.UserId) {
		sendError(w, http.StatusUnauthorized, errorcodes.NoPermission, "No permission to show this upload request")
		return
	}
	result, err := json.Marshal(uploadRequest)
	helper.Check(err)
	_, _ = w.Write(result)
}

func isAuthorisedForApi(r *http.Request, routing apiRoute) (models.User, models.ApiKey, bool) {
	keyId := r.Header.Get("apikey")
	if err := ratelimiter.WaitOnApiAuthentication(r.Context(), logging.GetIpAddress(r)); err != nil {
		return models.User{}, models.ApiKey{}, false
	}
	user, apiKey, ok := isValidApiKey(keyId, true, routing.ApiPerm)
	if !ok {
		return models.User{}, models.ApiKey{}, false
	}
	// Returns false if a public upload key is used for non-public api call or vice versa
	if routing.IsFileRequestApi != apiKey.IsUploadRequestKey() {
		return models.User{}, models.ApiKey{}, false
	}
	return user, apiKey, true
}

func sendError(w http.ResponseWriter, statusCode, errorCode int, errorMessage string) {
	if w == nil {
		return
	}
	w.WriteHeader(statusCode)
	output := struct {
		Result  string `json:"Result"`
		Message string `json:"ErrorMessage"`
		Code    int    `json:"ErrorCode"`
	}{Result: "error", Message: errorMessage, Code: errorCode}
	outputBytes, err := json.Marshal(output)
	helper.Check(err)
	_, _ = w.Write(outputBytes)
}

// publicKeyToApiKey tries to convert a (possible) public key to a private key
// If not a public key or if invalid, the original value is returned
func publicKeyToApiKey(publicKey string) string {
	if len(publicKey) == LengthPublicId {
		privateApiKey, ok := database.GetApiKeyByPublicKey(publicKey)
		if ok {
			return privateApiKey
		}
	}
	return publicKey
}

// isValidApiKey checks if the API key provides is valid. If modifyTime is true, it also automatically updates
// the lastUsed timestamp
func isValidApiKey(key string, modifyTime bool, requiredPermissionApiKey models.ApiPermission) (models.User, models.ApiKey, bool) {
	if key == "" {
		return models.User{}, models.ApiKey{}, false
	}
	savedKey, ok := database.GetApiKey(key)
	if ok && savedKey.Id != "" && (savedKey.Expiry == 0 || savedKey.Expiry > time.Now().Unix()) {
		if modifyTime {
			savedKey.LastUsed = time.Now().Unix()
			database.UpdateTimeApiKey(savedKey)
		}
		if !savedKey.HasPermission(requiredPermissionApiKey) {
			return models.User{}, models.ApiKey{}, false
		}
		user, ok := database.GetUser(savedKey.UserId)
		if !ok || !authorization.CanUseAPI(user, savedKey, requiredPermissionApiKey) {
			return models.User{}, models.ApiKey{}, false
		}
		return user, savedKey, true
	}
	return models.User{}, models.ApiKey{}, false
}
