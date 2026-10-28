//go:build !noaws && awsmock

package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/forceu/gokapi/internal/models"
)

var uploadedFiles []models.File
var isCorrectLogin bool
var mockStateMutex sync.RWMutex

const (
	region     = "mock-region-1"
	bucketName = "gokapi-test"
	accessId   = "accId"
	accessKey  = "accKey"
)

// IsIncludedInBuild is true if Gokapi has been compiled with AWS support or the API is being mocked
const IsIncludedInBuild = true

// IsMockApi is true if the API is being mocked and therefore can only be used for testing purposes
const IsMockApi = true

// Init reads the credentials for AWS
func Init(config models.AwsConfig) bool {
	if !isValidCredentials() {
		return false
	}
	Upload(bytes.NewReader([]byte("test")), models.File{
		Id:        "awsTest1234567890123",
		Name:      "aws Test File",
		Size:      "20 MB",
		SHA1:      "x341354656543213246465465465432456898794",
		AwsBucket: "gokapi-test",
	})
	return true
}

// IsAvailable returns true if valid credentials have been passed
func IsAvailable() bool {
	mockStateMutex.RLock()
	defer mockStateMutex.RUnlock()
	return isCorrectLogin
}

// IsValidLogin checks if a valid login was provided
func IsValidLogin(config models.AwsConfig) (bool, error) {
	return isValidCredentials(), nil
}

// LogOut resets the credentials
func LogOut() {
	mockStateMutex.Lock()
	defer mockStateMutex.Unlock()
	isCorrectLogin = false
}

// AddBucketName adds the bucket name to the file to be stored
func AddBucketName(file *models.File) {
	file.AwsBucket = bucketName
}

func isValidCredentials() bool {
	mockStateMutex.Lock()
	defer mockStateMutex.Unlock()
	requiredKeys := []string{"GOKAPI_AWS_BUCKET", "GOKAPI_AWS_REGION", "GOKAPI_AWS_KEY", "GOKAPI_AWS_KEY_SECRET"}
	requiredValues := []string{bucketName, region, accessId, accessKey}
	for i, key := range requiredKeys {
		val, _ := os.LookupEnv(key)
		if val != requiredValues[i] {
			isCorrectLogin = false
			return false
		}
	}
	isCorrectLogin = true
	return true
}

// IsCredentialProvided returns true if all credentials are provided, however does not check them to be valid
func IsCredentialProvided(checkIfValid bool) bool {
	requiredKeys := []string{"GOKAPI_AWS_BUCKET", "AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	for _, key := range requiredKeys {
		if !isValidEnv(key) {
			return false
		}
	}
	return true
}

func isValidEnv(key string) bool {
	val, ok := os.LookupEnv(key)
	return ok && val != ""
}

// Upload uploads a file to AWS
func Upload(input io.Reader, file models.File) (string, error) {
	if !isValidCredentials() {
		return "", errors.New("invalid credentials / invalid bucket / invalid region")
	}

	mockStateMutex.Lock()
	defer mockStateMutex.Unlock()
	if !isUploadedLocked(file) {
		uploadedFiles = append(uploadedFiles, file)
	}
	return "", nil
}

// UploadContext consumes the synthetic upload, unlike the legacy metadata-only
// Upload mock. It keeps memory bounded and checks cancellation between reads.
func UploadContext(ctx context.Context, input io.Reader, file models.File) (string, error) {
	if ctx == nil {
		return "", errors.New("invalid AWS context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input == nil {
		return "", errors.New("nil AWS upload input")
	}
	if !isValidCredentials() {
		return "", errors.New("invalid credentials / invalid bucket / invalid region")
	}
	size, err := io.Copy(io.Discard, uploadContextReader{ctx: ctx, reader: input})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file.SizeBytes = size
	mockStateMutex.Lock()
	defer mockStateMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for i, stored := range uploadedFiles {
		if stored.SHA1 == file.SHA1 && stored.AwsBucket == file.AwsBucket {
			uploadedFiles[i] = file
			return "", nil
		}
	}
	uploadedFiles = append(uploadedFiles, file)
	return "", nil
}

type uploadContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader uploadContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

// Download downloads a file from AWS
func download(writer io.WriterAt, file models.File) (int64, error) {
	if !isValidCredentials() {
		return 0, errors.New("invalid credentials / invalid bucket / invalid region")
	}

	if isUploaded(file) {
		return strconv.ParseInt(file.Size, 10, 64)
	}
	return 0, errors.New("file not found")
}

func isUploaded(file models.File) bool {
	mockStateMutex.RLock()
	defer mockStateMutex.RUnlock()
	return isUploadedLocked(file)
}

func isUploadedLocked(file models.File) bool {
	for _, element := range uploadedFiles {
		if element.SHA1 == file.SHA1 {
			return true
		}
	}
	return false
}

// ServeFile either redirects the user to a pre-signed download url (default) or downloads the file and serves it as a proxy (depending
// on configuration). Returns true if blocking operation (in order to set download status) or false if non-blocking.
func ServeFile(w http.ResponseWriter, r *http.Request, file models.File, forceDownload bool, forceDecryption bool) (bool, error) {
	// TODO implement proxy as well
	return false, RedirectToDownload(w, r, file, forceDownload)
}

// RedirectToDownload creates a presigned link that is valid for 15 seconds and redirects the
// client to this url
func RedirectToDownload(w http.ResponseWriter, r *http.Request, file models.File, forceDownload bool) error {
	if !isValidCredentials() {
		return errors.New("invalid credentials / invalid bucket / invalid region")
	}

	if isUploaded(file) {
		http.Redirect(w, r, "https://redirect.url", http.StatusTemporaryRedirect)
		return nil
	}
	return errors.New("file not found")
}

// FileExists returns true if the object is stored in S3
func FileExists(file models.File) (bool, int64, error) {
	if !isValidCredentials() {
		return false, 0, errors.New("invalid credentials / invalid bucket / invalid region")
	}
	if !isUploaded(file) {
		return false, 0, nil
	}

	return true, 10000, nil
}

// FileExistsContext reports the actual length recorded by UploadContext while
// the legacy FileExists mock retains its historical fixed-size response.
func FileExistsContext(ctx context.Context, file models.File) (bool, int64, error) {
	if ctx == nil {
		return false, 0, errors.New("invalid AWS context")
	}
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	if !isValidCredentials() {
		return false, 0, errors.New("invalid credentials / invalid bucket / invalid region")
	}
	mockStateMutex.RLock()
	defer mockStateMutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	for _, stored := range uploadedFiles {
		if stored.SHA1 == file.SHA1 && stored.AwsBucket == file.AwsBucket {
			if stored.SizeBytes < 0 {
				return false, 0, errors.New("invalid S3 object length")
			}
			return true, stored.SizeBytes, nil
		}
	}
	return false, 0, nil
}

// DeleteObject deletes a file from S3
func DeleteObject(file models.File) (bool, error) {
	if !isValidCredentials() {
		return false, errors.New("invalid credentials / invalid bucket / invalid region")
	}
	var buffer []models.File

	mockStateMutex.Lock()
	defer mockStateMutex.Unlock()
	for _, element := range uploadedFiles {
		if element.SHA1 != file.SHA1 || element.AwsBucket != file.AwsBucket {
			buffer = append(buffer, element)
		}
	}
	uploadedFiles = buffer

	return true, nil
}

// IsCorsCorrectlySet returns true if CORS rules allow download from Gokapi
func IsCorsCorrectlySet(bucket, gokapiUrl string) (bool, error) {
	switch bucket {
	case "any":
		return true, nil
	case "none":
		return false, nil
	case "forbidden":
		return false, errors.New("forbidden")
	case "https":
		return strings.HasPrefix(gokapiUrl, "https://"), nil
	case "url":
		return strings.HasPrefix(gokapiUrl, "http://test.com"), nil
	default:
		return false, errors.New("unknown")
	}
}

// GetDefaultBucketName returns the default bucketname where new files are stored
func GetDefaultBucketName() string {
	return bucketName
}

// Stream downloads a file from AWS sequentially, used for saving to a Zip file
func Stream(writer io.Writer, file models.File) error {
	if !isValidCredentials() {
		return errors.New("invalid credentials / invalid bucket / invalid region")
	}

	if isUploaded(file) {
		data, err := os.Open("data/" + file.SHA1)
		if err != nil {
			return err
		}
		defer data.Close()
		_, err = io.Copy(writer, data)
		return err
	}
	return errors.New("file not found")
}
