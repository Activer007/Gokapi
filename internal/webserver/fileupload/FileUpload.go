package fileupload

import (
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/environment"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/storage/chunking/chunksession"
	"github.com/forceu/gokapi/internal/storage/uploadreceive"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
)

const minChunkSize = 5 * 1024 * 1024
const minChunkSizeLowMaxChunk = 1 * 1024 * 1024

// ProcessCompleteFile processes a file upload request
// This is only used when a complete file is uploaded through the API with /files/add
// Normally a file is created from a chunk
func ProcessCompleteFile(w http.ResponseWriter, r *http.Request, userId, _ int) error {
	maxFile, err := uploadMiB(configuration.Get().MaxFileSizeMB)
	if err != nil {
		return err
	}
	upload, err := receiveUpload(w, r, maxFile)
	if err != nil {
		return err
	}
	defer closeReceived(upload)
	installUploadFields(r, upload.Fields)
	config, err := parseConfig(r.Form)
	if err != nil {
		return err
	}
	config.FileRequestId = ""
	if err := r.Context().Err(); err != nil {
		return err
	}
	limits, err := wholeUploadLimits(configuration.GetEnvironment())
	if err != nil {
		return wholeUploadFailure{err}
	}
	result, err := storage.NewFileReserved(r.Context(), upload.File, upload.Header, userId, config, limits, upload.Close)
	if err != nil {
		return wholeUploadFailure{err}
	}
	user, _ := database.GetUser(userId)
	logging.LogUpload(result, user, models.FileRequest{})
	_, _ = io.WriteString(w, result.ToJsonResult(config.ExternalUrl, configuration.Get().IncludeFilename))
	return nil
}

func isChunkMinChunkSize(chunkSize, offset, fileSize int64) bool {
	minReqChunkSize := minChunkSize
	if configuration.Get().ChunkSize < 5 {
		minReqChunkSize = minChunkSizeLowMaxChunk
	}
	if chunkSize >= int64(minReqChunkSize) {
		return true
	}
	if offset <= fileSize && chunkSize == fileSize-offset {
		return true
	}
	return false
}

// ProcessNewChunk processes a file chunk upload request
func ProcessNewChunk(w http.ResponseWriter, r *http.Request, isApiCall bool, filerequestId string, userId int, maxFileSize int64) (int, error) {
	// ChunkSize is a client tuning value. Browser E2E chunks include cipher
	// overhead; the file payload limit and shared receiver budget bound both.
	upload, err := receiveUpload(w, r, maxFileSize)
	if err != nil {
		return ReceiveErrorCode(err, errorcodes.CannotParse), err
	}
	defer closeReceived(upload)
	installUploadFields(r, upload.Fields)
	chunkInfo, err := chunking.ParseChunkInfo(r, isApiCall)
	if err != nil {
		return errorcodes.InvalidUserInput, err
	}
	if chunkInfo.Offset > chunkInfo.TotalFilesizeBytes || upload.Header.Size > chunkInfo.TotalFilesizeBytes-chunkInfo.Offset {
		return errorcodes.InvalidUserInput, errors.New("chunk exceeds declared file range")
	}
	if !isChunkMinChunkSize(upload.Header.Size, chunkInfo.Offset, chunkInfo.TotalFilesizeBytes) {
		return errorcodes.ChunkTooSmall, storage.ErrorChunkTooSmall
	}
	if chunkInfo.TotalFilesizeBytes > maxFileSize || chunkInfo.Offset > maxFileSize {
		return errorcodes.FileTooLarge, storage.ErrorFileTooLarge
	}

	if err := r.Context().Err(); err != nil {
		return errorcodes.InvalidUserInput, err
	}
	limits, err := chunkUploadLimits()
	if err != nil {
		err = wholeUploadFailure{err}
		return ReceiveErrorCode(err, errorcodes.CannotAllocateFile), err
	}
	_, err = storage.ChunkSessions().Write(r.Context(), chunksession.Scope{ClientID: chunkInfo.UUID, UserID: userId, RequestID: filerequestId}, chunkInfo, upload.File, upload.Header.Size, maxFileSize, limits)
	if err != nil {
		err = wholeUploadFailure{err}
		return ReceiveErrorCode(err, errorcodes.CannotAllocateFile), err
	}
	if err := upload.Close(); err != nil {
		return errorcodes.CannotAllocateFile, wholeUploadFailure{err}
	}
	_, _ = io.WriteString(w, "{\"result\":\"OK\"}")
	return 0, nil
}

var receiveOnce sync.Once
var receiver *uploadreceive.Pool
var receiverError error

func receiveUpload(w http.ResponseWriter, r *http.Request, maxFileBytes int64) (*uploadreceive.Upload, error) {
	receiveOnce.Do(func() {
		env := configuration.GetEnvironment()
		if err := env.ValidateReceiveLimits(); err != nil {
			receiverError = err
			return
		}
		receiver, receiverError = uploadreceive.New(env.UploadBodySlots, env.UploadReceiveMB*1024*1024)
	})
	if receiverError != nil {
		stopRejectedUpload(w, r)
		return nil, uploadreceive.ErrInvalid
	}
	// Keep the existing live free-space setting, including its legacy zero rule.
	minFree, err := uploadMiBOrZero(environment.New().MinFreeSpaceMB)
	if err != nil {
		stopRejectedUpload(w, r)
		return nil, err
	}
	upload, err := receiver.Receive(w, r, configuration.Get().DataDir, maxFileBytes, minFree)
	if err != nil {
		stopRejectedUpload(w, r)
	}
	return upload, err
}

// A rejected HTTP/1 body must not be drained by net/http after releasing our
// slot. Otherwise a short declared body can keep the rejection waiting on I/O.
func stopRejectedUpload(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now())
}

func uploadMiB(value int) (int64, error) {
	if value <= 0 {
		return 0, uploadreceive.ErrInvalid
	}
	return uploadMiBOrZero(value)
}

// MaxUploadBytes converts a configured per-file MiB limit without overflow.
func MaxUploadBytes(value int) (int64, error) { return uploadMiB(value) }

func uploadMiBOrZero(value int) (int64, error) {
	if value < 0 || int64(value) > math.MaxInt64/(1024*1024) {
		return 0, uploadreceive.ErrInvalid
	}
	return int64(value) * 1024 * 1024, nil
}

func installUploadFields(r *http.Request, fields url.Values) {
	r.PostForm = fields
	r.Form = r.URL.Query()
	for key, values := range fields {
		r.Form[key] = append(r.Form[key], values...)
	}
}

func closeReceived(upload *uploadreceive.Upload) {
	if err := upload.Close(); err != nil {
		// The pool retains the failed cleanup's charge and retries its own spool.
		log.Print("Upload receive cleanup pending")
	}
}

// ReceiveErrorStatus preserves ordinary validation responses and distinguishes
// finite receiver backpressure from permanent payload rejection.
func ReceiveErrorStatus(err error) int {
	var failure wholeUploadFailure
	if errors.As(err, &failure) {
		status, _, _ := WholeUploadErrorResponse(err)
		return status
	}
	switch {
	case errors.Is(err, uploadreceive.ErrBusy):
		return http.StatusTooManyRequests
	case errors.Is(err, uploadreceive.ErrCapacity), errors.Is(err, uploadreceive.ErrSpace):
		return http.StatusInsufficientStorage
	default:
		return http.StatusBadRequest
	}
}

func ReceiveErrorCode(err error, fallback int) int {
	var failure wholeUploadFailure
	if errors.As(err, &failure) {
		_, code, _ := WholeUploadErrorResponse(err)
		return code
	}
	switch {
	case errors.Is(err, uploadreceive.ErrBusy):
		return errorcodes.UploadBusy
	case errors.Is(err, uploadreceive.ErrCapacity), errors.Is(err, uploadreceive.ErrSpace):
		return errorcodes.CannotAllocateFile
	case errors.Is(err, uploadreceive.ErrTooLarge):
		return errorcodes.FileTooLarge
	default:
		return fallback
	}
}

// ParseFileHeader parses the parameters for CompleteChunk()
// This is done as two operations, as CompleteChunk can be blocking too long
// for an HTTP request, by calling this function first, r can be closed afterwards
func ParseFileHeader(r *http.Request) (string, chunking.FileHeader, models.UploadParameters, error) {
	err := r.ParseForm()
	if err != nil {
		return "", chunking.FileHeader{}, models.UploadParameters{}, err
	}
	chunkId := r.PostForm.Get("chunkid")
	config, err := parseConfig(r.Form)
	if err != nil {
		return "", chunking.FileHeader{}, models.UploadParameters{}, err
	}
	header, err := chunking.ParseFileHeader(r)
	if err != nil {
		return "", chunking.FileHeader{}, models.UploadParameters{}, err
	}
	return chunkId, header, config, nil
}

// CompleteChunk processes a file after all the chunks have been completed
// The parameters can be generated with  ParseFileHeader()
func CompleteChunk(chunkId string, header chunking.FileHeader, userId int, config models.UploadParameters) (models.File, error) {
	return storage.NewFileFromChunk(chunkId, header, userId, config)
}

// CreateUploadConfig populates a new models.UploadParameters struct
func CreateUploadConfig(allowedDownloads, expiryDays int, password string, unlimitedTime, unlimitedDownload, isEnd2End bool, realSize int64, fileRequestId string) models.UploadParameters {
	settings := configuration.Get()
	return models.UploadParameters{
		AllowedDownloads:    allowedDownloads,
		Expiry:              expiryDays,
		ExpiryTimestamp:     time.Now().Add(time.Duration(expiryDays) * time.Hour * 24).Unix(),
		Password:            password,
		ExternalUrl:         settings.ServerUrl,
		MaxMemory:           settings.MaxMemory,
		UnlimitedTime:       unlimitedTime,
		UnlimitedDownload:   unlimitedDownload,
		IsEndToEndEncrypted: isEnd2End,
		RealSize:            realSize,
		FileRequestId:       fileRequestId,
	}
}

func parseConfig(values formOrHeader) (models.UploadParameters, error) {
	fileRequestId := values.Get("fileRequestId")
	if fileRequestId != "" {
		return CreateUploadConfig(0, 0, "",
			true, true, false, 0, fileRequestId), nil
	}
	allowedDownloads := values.Get("allowedDownloads")
	expiryDays := values.Get("expiryDays")
	password := values.Get("password")
	allowedDownloadsInt, err := strconv.Atoi(allowedDownloads)
	if err != nil {
		allowedDownloadsInt = 1
	}
	expiryDaysInt, err := strconv.Atoi(expiryDays)
	if err != nil {
		expiryDaysInt = 14
	}

	unlimitedDownload := values.Get("isUnlimitedDownload") == "true"
	unlimitedTime := values.Get("isUnlimitedTime") == "true"

	if allowedDownloadsInt == 0 {
		unlimitedDownload = true
	}
	if expiryDaysInt == 0 {
		unlimitedTime = true
	}

	var isEnd2End bool
	var realSize int64
	if values.Get("isE2E") == "true" {
		isEnd2End = true
		realSizeStr := values.Get("realSize")
		realSize, err = strconv.ParseInt(realSizeStr, 10, 64)
		if err != nil {
			return models.UploadParameters{}, err
		}
	}
	return CreateUploadConfig(allowedDownloadsInt, expiryDaysInt, password, unlimitedTime, unlimitedDownload, isEnd2End, realSize, ""), nil
}

type formOrHeader interface {
	Get(key string) string
}
