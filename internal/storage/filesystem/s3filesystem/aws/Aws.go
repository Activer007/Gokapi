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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/headers"
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
	sess, err := createSession()
	if err != nil {
		return err
	}
	s3svc := s3.New(sess)

	obj, err := s3svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		return err
	}
	defer obj.Body.Close()

	var reader io.Reader = obj.Body

	if file.Encryption.IsEncrypted {
		return encryption.DecryptReader(file.Encryption, obj.Body, writer)
	}
	_, err = io.Copy(writer, reader)
	return err
}

// ServeFile either redirects the user to a pre-signed download url (default) or downloads the file and serves it as a proxy (depending
// on configuration). Returns true if blocking operation (to set download status) or false if non-blocking.
func ServeFile(w http.ResponseWriter, r *http.Request, file models.File, forceDownload, forceDecryption bool) (bool, error) {
	if forceDecryption {
		return true, serveDecryptedFile(w, file)
	}
	if awsConfig.ProxyDownload {
		return true, proxyDownload(w, file, forceDownload)
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
func proxyDownload(w http.ResponseWriter, file models.File, forceDownload bool) error {
	url, err := getPresignedUrl(file, forceDownload)
	if err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	headers.Write(file, w, forceDownload, false)
	_, _ = io.Copy(w, resp.Body)
	return nil
}

func serveDecryptedFile(w http.ResponseWriter, file models.File) error {
	sess, err := createSession()
	if err != nil {
		return err
	}
	s3svc := s3.New(sess)

	// 1. Get the object from S3
	obj, err := s3svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})
	if err != nil {
		return err
	}
	defer obj.Body.Close()

	headers.Write(file, w, true, true)
	if file.Encryption.IsEncrypted {
		if !encryption.IsDecryptionAvailable() {
			return errors.New("file is encrypted but server-side decryption key is not available")
		}
		return encryption.DecryptReader(file.Encryption, obj.Body, w)
	}
	_, err = io.Copy(w, obj.Body)
	return err
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

// DeleteObject deletes a file from S3
func DeleteObject(file models.File) (bool, error) {
	sess, err := createSession()
	if err != nil {
		return false, err
	}
	svc := s3.New(sess)

	ctx, cancelCtx := getTimeoutContext()
	defer cancelCtx()

	_, err = svc.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(file.AwsBucket),
		Key:    aws.String(file.SHA1),
	})

	if err != nil {
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
