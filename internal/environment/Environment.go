package environment

import (
	"fmt"
	"math"
	"os"
	"path"
	"reflect"
	"strconv"

	envParser "github.com/caarlos0/env/v6"
	"github.com/forceu/gokapi/internal/environment/deprecation"
	"github.com/forceu/gokapi/internal/environment/flagparser"
	"github.com/forceu/gokapi/internal/helper"
)

// DefaultPort for the webserver
const DefaultPort = 53842

// MinLengthId is the minimum character length for download and file request IDs
// The value is hardcoded in Environment and needs to be kept in sync with this value.
const MinLengthId = 5

// Environment is a struct containing available env variables
type Environment struct {
	// Sets the directory for the config file
	ConfigDir string `env:"CONFIG_DIR" envDefault:"config"`
	// Sets the name of the config file
	ConfigFile string `env:"CONFIG_FILE" envDefault:"config.json"`
	// The full path to the config file
	ConfigPath string
	// Sets the directory for the data
	DataDir string `env:"DATA_DIR" envDefault:"data" persistent:"true"`
	// Disables the API menu and generation of API keys for non-admin users
	DisableApiMenu bool `env:"DISABLE_API_MENU" envDefault:"false"`
	// Disables the CORS check on startup and during setup, if set to true
	DisableCorsCheck bool `env:"DISABLE_CORS_CHECK" envDefault:"false"`
	// Disables automatic Docker subnet trust by default; false is a legacy opt-in.
	DisableDockerTrustedProxy bool `env:"DISABLE_DOCKER_TRUSTED_PROXY" envDefault:"true"`
	// Sets the size of chunks that are uploaded in MB
	ChunkSizeMB int `env:"CHUNK_SIZE_MB" envDefault:"45" onlyPositive:"true" persistent:"true"`
	// Sets the length of the download IDs
	LengthId int `env:"LENGTH_ID" envDefault:"15" minValue:"5"`
	// Sets the length of the hotlink IDs
	LengthHotlinkId int `env:"LENGTH_HOTLINK_ID" envDefault:"40" minValue:"8"`
	// Also outputs all log file entries to the console output, if set to true
	LogToStdout bool `env:"LOG_STDOUT" envDefault:"false"`
	// Sets the maximum allowed file size in MB
	// Default 102400 = 100GB
	MaxFileSize int `env:"MAX_FILESIZE" envDefault:"102400" onlyPositive:"true" persistent:"true"`
	// Sets the amount of RAM in MB that can be allocated for an upload chunk or file
	// Any chunk or file with a size greater than that will be written to a temporary file
	MaxMemory int `env:"MAX_MEMORY_UPLOAD" envDefault:"50" onlyPositive:"true" persistent:"true"`
	// Bounds concurrent request bodies during upload reception, independently
	// of durable upload IDs and the number of parallel chunks per client.
	UploadBodySlots int64 `env:"UPLOAD_BODY_SLOTS" envDefault:"4"`
	// Bounds transient upload reception and processing copies in MiB. This is
	// not the persisted chunk ledger or a user's cumulative logical quota.
	UploadReceiveMB int64 `env:"UPLOAD_RECEIVE_MB" envDefault:"12288"`

	// Logical owner limits apply across all of that owner's upload credentials.
	UploadUserTotalMB  int64 `env:"UPLOAD_USER_TOTAL_MB" envDefault:"5120"`
	UploadUserMaxFiles int64 `env:"UPLOAD_USER_MAX_FILES" envDefault:"1000"`
	UploadUserActive   int64 `env:"UPLOAD_USER_ACTIVE" envDefault:"4"`

	// Instance logical limits are independent of actual disk free space.
	UploadInstanceTotalMB  int64 `env:"UPLOAD_INSTANCE_TOTAL_MB" envDefault:"20480"`
	UploadInstanceMaxFiles int64 `env:"UPLOAD_INSTANCE_MAX_FILES" envDefault:"5000"`
	UploadInstanceActive   int64 `env:"UPLOAD_INSTANCE_ACTIVE" envDefault:"8"`

	// Bounds durable staging declarations, separately from reception copies.
	UploadStagingMB int64 `env:"UPLOAD_STAGING_MB" envDefault:"12288"`

	// File Requests share these finite cumulative and active capacities. Their
	// existing MaxSize remains a separate per-file restriction.
	UploadRequestTotalMB int64 `env:"UPLOAD_REQUEST_TOTAL_MB" envDefault:"20480"`
	UploadRequestActive  int64 `env:"UPLOAD_REQUEST_ACTIVE" envDefault:"2"`
	// Sets the maximum number of files that can be uploaded per file requests created by
	// non-admin users
	// Set to 0 to allow unlimited file count for all users
	MaxFilesGuestUpload int `env:"MAX_FILES_GUESTUPLOAD" envDefault:"100" onlyPositive:"true"`
	// Sets the maximum file size for file requests created by
	// non-admin users
	// Set to 0 to allow files with a size of up to a value set with GOKAPI_MAX_FILESIZE
	// for all users
	// Default 10240 = 10GB
	MaxSizeGuestUploadMb int `env:"MAX_SIZE_GUESTUPLOAD" envDefault:"10240" onlyPositive:"true"`
	// Set the number of chunks that are uploaded in parallel for a single file
	MaxParallelUploads int `env:"MAX_PARALLEL_UPLOADS" envDefault:"3" onlyPositive:"true" persistent:"true"`
	// Sets the minimum free space on the disk in MB for accepting an upload
	MinFreeSpaceMB int `env:"MIN_FREE_SPACE" envDefault:"400" onlyPositive:"true"`
	// Sets the minimum password length
	MinLengthPassword int `env:"MIN_LENGTH_PASSWORD" envDefault:"8" minValue:"6"`
	// Allows all users by default to create file requests, if set to true
	PermRequestGrantedByDefault bool `env:"GUEST_UPLOAD_BY_DEFAULT" envDefault:"false"`
	// Sets a list of trusted proxies. If set, the webserver will trust the IP addresses sent
	// by these proxies with the X-Forwarded-For and X-REAL-IP header
	// List is comma separated; entries can be fixed IPs ("10.0.0.1, 10.0.0.2")
	// and subnets ("10.0.0.0/24")
	TrustedProxies []string `env:"TRUSTED_PROXIES" envSeparator:"," envDefault:"127.0.0.1"`
	// Set this to true if you are using Cloudflare
	UseCloudFlare bool `env:"USE_CLOUDFLARE" envDefault:"false"`
	// Sets the webserver port
	WebserverPort int `env:"PORT" envDefault:"53842" onlyPositive:"true" persistent:"true"`
	// Allow hotlinking of videos. Note: Due to buffering, playing a video might count as
	// multiple downloads. It is only recommended to use video hotlinking for uploads with
	// unlimited downloads enabled
	HotlinkVideos bool `env:"ENABLE_HOTLINK_VIDEOS" envDefault:"false"`
	// Sets the AWS bucket name
	AwsBucket string `env:"AWS_BUCKET"`
	// Sets the AWS region name
	AwsRegion string `env:"AWS_REGION"`
	// Sets the AWS API key
	AwsKeyId string `env:"AWS_KEY"`
	// Sets the AWS API secret
	AwsKeySecret string `env:"AWS_KEY_SECRET"`
	// Sets the AWS endpoint
	AwsEndpoint string `env:"AWS_ENDPOINT"`
	// Proxies downloads through the server instead of redirecting to pre-signed S3 URLs, if set to true
	AwsProxyDownload bool `env:"AWS_PROXY_DOWNLOAD" envDefault:"false"`
	// List of active deprecations
	ActiveDeprecations []deprecation.Deprecation
	isSet              bool
}

// IsParsed returns true if the env variables have been parsed
func (e *Environment) IsParsed() bool {
	return e.isSet
}

// ValidateReceiveLimits rejects invalid explicit reception capacities. Legacy
// upload settings retain their separate zero and normalization semantics.
func (e Environment) ValidateReceiveLimits() error {
	if e.UploadBodySlots <= 0 || e.UploadBodySlots > 64 {
		return fmt.Errorf("GOKAPI_UPLOAD_BODY_SLOTS must be between 1 and 64")
	}
	if e.UploadReceiveMB <= 0 || e.UploadReceiveMB > math.MaxInt64/(1024*1024) {
		return fmt.Errorf("GOKAPI_UPLOAD_RECEIVE_MB must be positive and fit an int64 byte budget")
	}
	return nil
}

// ValidateWholeUploadLimits checks finite whole-upload capacities without
// normalizing explicit input or changing reception-only fixture validation.
func (e Environment) ValidateWholeUploadLimits() error {
	for _, field := range []struct {
		name  string
		value int64
		bytes bool
	}{
		{"GOKAPI_UPLOAD_USER_TOTAL_MB", e.UploadUserTotalMB, true},
		{"GOKAPI_UPLOAD_USER_MAX_FILES", e.UploadUserMaxFiles, false},
		{"GOKAPI_UPLOAD_USER_ACTIVE", e.UploadUserActive, false},
		{"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB", e.UploadInstanceTotalMB, true},
		{"GOKAPI_UPLOAD_INSTANCE_MAX_FILES", e.UploadInstanceMaxFiles, false},
		{"GOKAPI_UPLOAD_INSTANCE_ACTIVE", e.UploadInstanceActive, false},
		{"GOKAPI_UPLOAD_STAGING_MB", e.UploadStagingMB, true},
	} {
		if field.value <= 0 {
			return fmt.Errorf("%s must be positive", field.name)
		}
		if field.bytes && field.value > math.MaxInt64/(1024*1024) {
			return fmt.Errorf("%s must fit an int64 byte budget", field.name)
		}
	}
	return nil
}

// ValidateRequestUploadLimits is separate from reception and whole-upload
// validation so existing synthetic configurations keep their original scope.
func (e Environment) ValidateRequestUploadLimits() error {
	if e.UploadRequestTotalMB <= 0 || e.UploadRequestTotalMB > math.MaxInt64/(1024*1024) {
		return fmt.Errorf("GOKAPI_UPLOAD_REQUEST_TOTAL_MB must be positive and fit an int64 byte budget")
	}
	if e.UploadRequestActive <= 0 {
		return fmt.Errorf("GOKAPI_UPLOAD_REQUEST_ACTIVE must be positive")
	}
	return nil
}

// New parses the env variables
func New() Environment {
	result := Environment{
		WebserverPort: DefaultPort,
		isSet:         true,
	}

	result = parseEnvVars(result)
	if !result.isSet {
		return Environment{}
	}
	err := enforceIntLimits(&result)
	if err != nil {
		fmt.Println("Error parsing env variables:", err)
		osExit(1)
		return Environment{}
	}
	result = parseFlags(result)
	result.ActiveDeprecations = deprecation.GetActive()

	return result
}

func parseEnvVars(result Environment) Environment {
	// Validate strict capacities before the general parser: its error
	// messages include raw supplied values, which must not reach logs here.
	validated, err := parseReceiveEnvironment(result)
	if err != nil {
		fmt.Println("Error parsing env variables:", err)
		osExit(1)
		return Environment{}
	}
	validated, err = parseWholeUploadEnvironment(validated)
	if err != nil {
		fmt.Println("Error parsing env variables:", err)
		osExit(1)
		return Environment{}
	}
	validated, err = parseRequestUploadEnvironment(validated)
	if err != nil {
		fmt.Println("Error parsing env variables:", err)
		osExit(1)
		return Environment{}
	}
	result = validated
	err = envParser.Parse(&result, envParser.Options{
		Prefix: "GOKAPI_",
	})
	if err != nil {
		fmt.Println("Error parsing env variables:", err)
		osExit(1)
		return Environment{}
	}
	return result
}

func parseReceiveEnvironment(result Environment) (Environment, error) {
	result.UploadBodySlots, result.UploadReceiveMB = 4, 12288
	for _, field := range []struct {
		name   string
		target *int64
	}{
		{name: "GOKAPI_UPLOAD_BODY_SLOTS", target: &result.UploadBodySlots},
		{name: "GOKAPI_UPLOAD_RECEIVE_MB", target: &result.UploadReceiveMB},
	} {
		if raw, exists := os.LookupEnv(field.name); exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return Environment{}, fmt.Errorf("invalid integer configuration for %s", field.name)
			}
			*field.target = value
		}
	}
	if err := result.ValidateReceiveLimits(); err != nil {
		return Environment{}, err
	}
	return result, nil
}

func parseWholeUploadEnvironment(result Environment) (Environment, error) {
	result.UploadUserTotalMB, result.UploadUserMaxFiles, result.UploadUserActive = 5120, 1000, 4
	result.UploadInstanceTotalMB, result.UploadInstanceMaxFiles, result.UploadInstanceActive = 20480, 5000, 8
	result.UploadStagingMB = 12288
	for _, field := range []struct {
		name   string
		target *int64
	}{
		{"GOKAPI_UPLOAD_USER_TOTAL_MB", &result.UploadUserTotalMB},
		{"GOKAPI_UPLOAD_USER_MAX_FILES", &result.UploadUserMaxFiles},
		{"GOKAPI_UPLOAD_USER_ACTIVE", &result.UploadUserActive},
		{"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB", &result.UploadInstanceTotalMB},
		{"GOKAPI_UPLOAD_INSTANCE_MAX_FILES", &result.UploadInstanceMaxFiles},
		{"GOKAPI_UPLOAD_INSTANCE_ACTIVE", &result.UploadInstanceActive},
		{"GOKAPI_UPLOAD_STAGING_MB", &result.UploadStagingMB},
	} {
		if raw, exists := os.LookupEnv(field.name); exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return Environment{}, fmt.Errorf("invalid integer configuration for %s", field.name)
			}
			*field.target = value
		}
	}
	if err := result.ValidateWholeUploadLimits(); err != nil {
		return Environment{}, err
	}
	return result, nil
}

func parseRequestUploadEnvironment(result Environment) (Environment, error) {
	result.UploadRequestTotalMB, result.UploadRequestActive = 20480, 2
	for _, field := range []struct {
		name   string
		target *int64
	}{
		{"GOKAPI_UPLOAD_REQUEST_TOTAL_MB", &result.UploadRequestTotalMB},
		{"GOKAPI_UPLOAD_REQUEST_ACTIVE", &result.UploadRequestActive},
	} {
		if raw, exists := os.LookupEnv(field.name); exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return Environment{}, fmt.Errorf("invalid integer configuration for %s", field.name)
			}
			*field.target = value
		}
	}
	if err := result.ValidateRequestUploadLimits(); err != nil {
		return Environment{}, err
	}
	return result, nil
}

func enforceIntLimits(result *Environment) error {
	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("env must be a pointer to a struct")
	}
	if err := result.ValidateReceiveLimits(); err != nil {
		return err
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldVal := v.Field(i)
		fieldType := t.Field(i)

		checkForPositive := fieldType.Tag.Get("onlyPositive") != ""
		checkForMinValue := fieldType.Tag.Get("minValue") != ""

		if !checkForPositive && !checkForMinValue {
			continue
		}

		// Only handle signed integers
		switch fieldVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

			if checkForMinValue {
				valStr := fieldType.Tag.Get("minValue")
				minVal, err := strconv.ParseInt(valStr, 10, fieldVal.Type().Bits())
				if err != nil {
					return fmt.Errorf("invalid minValue for field %s: %w", fieldType.Name, err)
				}
				if fieldVal.Int() < minVal {
					if fieldVal.CanSet() {
						fieldVal.SetInt(minVal)
					} else {
						return fmt.Errorf("cannot set fieldval %s", fieldType.Name)
					}
					continue
				}
			}

			if !checkForPositive {
				continue
			}
			if fieldVal.Int() >= 0 {
				continue
			}

			defaultStr := fieldType.Tag.Get("envDefault")
			defaultVal, err := strconv.ParseInt(defaultStr, 10, fieldVal.Type().Bits())
			if err != nil {
				return fmt.Errorf("invalid envDefault for field %s: %w", fieldType.Name, err)
			}

			if fieldVal.CanSet() {
				fieldVal.SetInt(defaultVal)
			} else {
				return fmt.Errorf("cannot set fieldval %s", fieldType.Name)
			}
		default:
			continue
		}
	}
	return nil
}

func parseFlags(result Environment) Environment {
	flags := flagparser.ParseFlags()
	if flags.IsPortSet {
		if flags.Port < 1 {
			flags.Port = DefaultPort
		}
		result.WebserverPort = flags.Port
	}
	if flags.IsConfigDirSet {
		result.ConfigDir = flags.ConfigDir
	}
	if flags.IsDataDirSet {
		result.DataDir = flags.DataDir
	}

	result.ConfigDir = path.Clean(result.ConfigDir)
	result.DataDir = path.Clean(result.DataDir)
	result.ConfigPath = result.ConfigDir + "/" + result.ConfigFile
	if flags.IsConfigPathSet {
		result.ConfigPath = flags.ConfigPath
	}

	if IsDockerInstance() && os.Getenv("TMPDIR") == "" {
		err := os.Setenv("TMPDIR", result.DataDir)
		helper.Check(err)
	}
	if result.LengthId < 5 {
		result.LengthId = 5
	}
	if result.LengthHotlinkId < 8 {
		result.LengthHotlinkId = 8
	}
	if result.MaxMemory < 5 {
		result.MaxMemory = 5
	}
	if result.MaxFileSize < 1 {
		result.MaxFileSize = 5
	}
	if result.MinLengthPassword < 6 {
		result.MinLengthPassword = 6
	}
	return result
}

// IsAwsProvided returns true if all required env variables have been set for using AWS S3 / Backblaze
func (e *Environment) IsAwsProvided() bool {
	return e.AwsBucket != "" &&
		e.AwsRegion != "" &&
		e.AwsKeyId != "" &&
		e.AwsKeySecret != ""
}

// GetConfigPaths returns the config paths to config files and the directory containing the files. The following results are returned:
// Path to config file, Path to directory containing config file, Name of config file, Path to AWS config file
func GetConfigPaths() (pathConfigFile, pathConfigDir, nameConfigFile, pathAwsConfig string) {
	env := New()
	pathAwsConfig = env.ConfigDir + "/cloudconfig.yml"
	return env.ConfigPath, env.ConfigDir, env.ConfigFile, pathAwsConfig
}

var osExit = os.Exit
