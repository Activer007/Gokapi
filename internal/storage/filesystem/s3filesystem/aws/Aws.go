//go:build !noaws && !awsmock

package aws

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/headers"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

var awsConfig models.AwsConfig

var isCorrectLogin bool

// IsIncludedInBuild is true if SupaShare has been compiled with AWS support or the API is being mocked
const IsIncludedInBuild = true

// IsMockApi is true if the API is being mocked and therefore can only be used for testing purposes
const IsMockApi = false

// Init reads the credentials for AWS. Returns true if valid
func Init(config models.AwsConfig) bool {
	awsConfig = config
	ok, err := IsValidLogin(config)
	if err != nil {
		fmt.Println("WARNING: AWS login not successful")
		fmt.Println(err.Error())
		isCorrectLogin = false
		return false
	}
	if ok {
		fmt.Println("AWS login successful")
		isCorrectLogin = true
	}
	return ok
}

// AddBucketName adds the bucket name to the file to be stored
func AddBucketName(file *models.File) {
	file.AwsBucket = awsConfig.Bucket
}

// IsAvailable returns true if valid credentials have been passed
func IsAvailable() bool {
	return isCorrectLogin
}

// LogOut resets the credentials
func LogOut() {
	awsConfig = models.AwsConfig{}
	isCorrectLogin = false
}

// IsValidLogin checks if a valid login was provided
func IsValidLogin(config models.AwsConfig) (bool, error) {
	if !config.IsAllProvided() {
		return false, nil
	}
	tempConfig := awsConfig
	awsConfig = config
	_, _, err := FileExists(models.File{AwsBucket: awsConfig.Bucket, SHA1: "invalid"})
	awsConfig = tempConfig
	if err != nil {
		return false, err
	}
	return true, nil
}

func createSession() (*session.Session, error) {
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(awsConfig.KeyId, awsConfig.KeySecret, ""),
		Endpoint:         aws.String(awsConfig.Endpoint),
		Region:           aws.String(awsConfig.Region),
		S3ForcePathStyle: aws.Bool(true),
	}
	return session.NewSession(s3Config)
}

// Upload uploads a file to AWS
func Upload(input io.Reader, file models.File) (string, error) {
	return UploadContext(context.Background(), input, file)
}

// UploadContext uploads using the caller's cancellation and deadline.
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
	sess, err := createSession()
	if err != nil {
		return "", err
	}
	uploader := s3manager.NewUploader(sess)

	result, err := uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
		Body:   input,
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		return "", err
	}
	return result.Location, nil
}

// download downloads a file from AWS, used for testing
func download(writer io.WriterAt, file models.File) (int64, error) {
	sess, err := createSession()
	if err != nil {
		return 0, err
	}
	downloader := s3manager.NewDownloader(sess)
	size, err := downloader.Download(writer, &s3.GetObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		return 0, err
	}
	return size, nil
}

// Stream downloads a file from AWS sequentially, used for saving to a Zip file
func Stream(writer io.Writer, file models.File) error {
	return StreamContext(context.Background(), writer, file)
}

// StreamContext preserves cancellation while streaming a verified object. The
// legacy Stream wrapper remains available to callers without a request context.
func StreamContext(ctx context.Context, writer io.Writer, file models.File) error {
	if err := validateDownloadContext(ctx); err != nil {
		return err
	}
	sourceSize, err := downloadSourceSize(file)
	if err != nil {
		return err
	}
	var key []byte
	if file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted {
		key, err = downloadDecryptionKey(file)
		if err != nil {
			return err
		}
	}
	sess, err := createSession()
	if err != nil {
		return err
	}
	s3svc := s3.New(sess)

	obj, err := s3svc.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		return downloadContextError(ctx, err)
	}
	defer obj.Body.Close()
	if obj.ContentLength != nil {
		if err := checkDownloadLength(*obj.ContentLength, sourceSize); err != nil {
			return err
		}
	}
	return copyDownload(ctx, writer, obj.Body, file, sourceSize, key)
}

// ServeFile either redirects the user to a pre-signed download url (default) or downloads the file and serves it as a proxy (depending
// on configuration). Returns true if blocking operation (to set download status) or false if non-blocking.
func ServeFile(w http.ResponseWriter, r *http.Request, file models.File, forceDownload, forceDecryption bool) (bool, error) {
	if forceDecryption {
		return true, serveDecryptedFile(w, r, file)
	}
	if awsConfig.ProxyDownload {
		return true, proxyDownload(w, r, file, forceDownload)
	}
	return false, redirectToDownload(w, r, file, forceDownload)
}

func getPresignedUrl(file models.File, forceDownload bool) (string, error) {
	sess, err := createSession()
	if err != nil {
		return "", err
	}
	s3svc := s3.New(sess)

	dispositionType := "inline"
	if forceDownload {
		dispositionType = "Attachment"
	}
	// Use RFC 6266 format to support UTF-8 filenames and avoid encoding errors.
	contentDisposition := fmt.Sprintf("%s; filename*=UTF-8''%s", dispositionType, url.PathEscape(file.Name))

	req, _ := s3svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket:                     aws.String(file.AwsBucket),
		Key:                        aws.String(file.SHA1),
		ResponseContentDisposition: aws.String(contentDisposition),
		ResponseCacheControl:       aws.String("no-store"),
		ResponseContentType:        aws.String(file.ContentType),
	})

	return req.Presign(15 * time.Second)
}

// redirectToDownload creates a presigned link that is valid for 15 seconds and redirects the
// client to this url
func redirectToDownload(w http.ResponseWriter, r *http.Request, file models.File, forceDownload bool) error {
	url, err := getPresignedUrl(file, forceDownload)
	if err != nil {
		return err
	}

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	return nil
}

// proxyDownload streams the file from S3 as a proxy, by downloading a presigned url
func proxyDownload(w http.ResponseWriter, r *http.Request, file models.File, forceDownload bool) error {
	ctx := r.Context()
	if err := validateDownloadContext(ctx); err != nil {
		return err
	}
	sourceSize, err := downloadSourceSize(file)
	if err != nil {
		return err
	}
	url, err := getPresignedUrl(file, forceDownload)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// The stored object length refers to its bytes, not transparent HTTP gzip.
	request.Header.Set("Accept-Encoding", "identity")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return downloadContextError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("S3 download returned status %d", resp.StatusCode)
	}
	if err := checkDownloadLength(resp.ContentLength, sourceSize); err != nil {
		return err
	}
	headers.Write(file, w, forceDownload, false)
	return copyDownload(ctx, w, resp.Body, file, sourceSize, nil)
}

func serveDecryptedFile(w http.ResponseWriter, r *http.Request, file models.File) error {
	ctx := r.Context()
	if err := validateDownloadContext(ctx); err != nil {
		return err
	}
	sourceSize, err := downloadSourceSize(file)
	if err != nil {
		return err
	}
	var key []byte
	if file.Encryption.IsEncrypted {
		key, err = downloadDecryptionKey(file)
		if err != nil {
			return err
		}
	}
	sess, err := createSession()
	if err != nil {
		return err
	}
	s3svc := s3.New(sess)

	// 1. Get the object from S3
	obj, err := s3svc.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		return downloadContextError(ctx, err)
	}
	defer obj.Body.Close()
	if obj.ContentLength != nil {
		if err := checkDownloadLength(*obj.ContentLength, sourceSize); err != nil {
			return err
		}
	}
	headers.Write(file, w, true, true)
	return copyDownload(ctx, w, obj.Body, file, sourceSize, key)
}

func validateDownloadContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("invalid S3 download context")
	}
	return ctx.Err()
}

func downloadContextError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	return err
}

func downloadSourceSize(file models.File) (int64, error) {
	if file.SizeBytes < 0 {
		return 0, errors.New("invalid S3 download length")
	}
	size := file.SizeBytes
	if file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted {
		size = encryption.CalculateEncryptedFilesize(file.SizeBytes)
		if size < file.SizeBytes {
			return 0, errors.New("invalid S3 encrypted download length")
		}
	}
	return size, nil
}

func downloadDecryptionKey(file models.File) ([]byte, error) {
	if file.Encryption.IsEndToEndEncrypted || !encryption.IsDecryptionAvailable() || len(file.Encryption.Nonce) != 12 {
		return nil, errors.New("server-side decryption is unavailable")
	}
	key, err := encryption.GetCipherFromFile(file.Encryption)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid S3 download decryption key")
	}
	return key, nil
}

// Unknown HTTP lengths are checked against the actual read count at EOF.
func checkDownloadLength(actual, expected int64) error {
	if actual >= 0 && actual != expected {
		return errors.New("S3 download length does not match metadata")
	}
	return nil
}

type downloadReader struct {
	ctx context.Context
	io.Reader
	count int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.Reader.Read(p)
	r.count += int64(n)
	return n, err
}

type downloadWriter struct {
	ctx context.Context
	io.Writer
	count int64
}

func (w *downloadWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.Writer.Write(p)
	w.count += int64(n)
	return n, err
}

func copyDownload(ctx context.Context, writer io.Writer, body io.Reader, file models.File, sourceSize int64, key []byte) error {
	if writer == nil {
		return errors.New("invalid S3 download writer")
	}
	source := &downloadReader{ctx: ctx, Reader: body}
	output := &downloadWriter{ctx: ctx, Writer: writer}
	var reader io.Reader = source
	outputSize := sourceSize
	if key != nil {
		var err error
		reader, err = encryption.GetDecryptReader(key, source)
		if err != nil {
			return err
		}
		outputSize = file.SizeBytes
	}
	if _, err := io.Copy(output, reader); err != nil {
		return downloadContextError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if source.count < sourceSize || output.count < outputSize {
		return io.ErrUnexpectedEOF
	}
	if source.count > sourceSize || output.count > outputSize {
		return errors.New("S3 download exceeds metadata length")
	}
	return nil
}

func getTimeoutContext() (context.Context, context.CancelFunc) {
	ctx := context.Background()
	rContext, rCancel := context.WithTimeout(ctx, 5*time.Second)
	return rContext, func() {
		if rCancel != nil {
			rCancel()
		}
	}
}

// FileExists returns true if the object is stored in S3
func FileExists(file models.File) (bool, int64, error) {
	return FileExistsContext(context.Background(), file)
}

// FileExistsContext fails closed on lookup errors. A canceled caller is not
// replaced by a fresh background timeout, and an absent object is distinct from
// an object whose existence or length could not be verified.
func FileExistsContext(ctx context.Context, file models.File) (bool, int64, error) {
	if ctx == nil {
		return false, 0, errors.New("invalid AWS context")
	}
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	sess, err := createSession()
	if err != nil {
		return false, 0, err
	}
	svc := s3.New(sess)

	ctx, cancelCtx := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCtx()

	info, err := svc.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})

	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, 0, contextErr
		}
		var aerr awserr.Error
		ok := errors.As(err, &aerr)
		if ok {
			if aerr.Code() == "NotFound" {
				return false, 0, nil
			}
		}
		return false, 0, err
	}
	if info == nil || info.ContentLength == nil || *info.ContentLength < 0 {
		return false, 0, errors.New("invalid S3 object length")
	}
	return true, *info.ContentLength, nil
}

// DeleteObject deletes a file from S3 using the legacy request timeout.
func DeleteObject(file models.File) (bool, error) {
	return DeleteObjectContext(context.Background(), file)
}

// DeleteObjectContext preserves caller cancellation and the five-second request
// cap. Errors are returned to the caller without logging object identifiers.
func DeleteObjectContext(ctx context.Context, file models.File) (bool, error) {
	if ctx == nil {
		return false, errors.New("invalid AWS context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	sess, err := createSession()
	if err != nil {
		return false, err
	}
	svc := s3.New(sess)

	ctx, cancelCtx := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCtx()

	_, err = svc.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, err
	}
	return true, nil
}

// IsCorsCorrectlySet returns true if CORS rules allow download from SupaShare
func IsCorsCorrectlySet(bucket, gokapiUrl string) (bool, error) {
	sess, err := createSession()
	if err != nil {
		return false, err
	}
	svc := s3.New(sess)
	input := &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	}

	ctx, cancelCtx := getTimeoutContext()
	defer cancelCtx()

	result, err := svc.GetBucketCorsWithContext(ctx, input)
	if err != nil {
		var aerr awserr.Error
		ok := errors.As(err, &aerr)
		if ok && (aerr.Code() == "NoSuchCORSConfiguration" ||
			aerr.Code() == "NoSuchCorsConfiguration") {
			return false, nil
		}
		return false, err
	}

	for _, rule := range result.CORSRules {
		for _, origin := range rule.AllowedOrigins {
			if *origin == "*" {
				return true, nil
			}
			if strings.HasPrefix(gokapiUrl, *origin) {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetDefaultBucketName returns the default bucketname where new files are stored
func GetDefaultBucketName() string {
	return awsConfig.Bucket
}
