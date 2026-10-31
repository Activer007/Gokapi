//go:build go1.25

package main

/**
Main routine
*/

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database/migration"
	"github.com/Activer007/SupaShare/internal/helper/systemd"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/cloudconfig"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/configuration/setup"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/environment/flagparser"
	"github.com/Activer007/SupaShare/internal/logging"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/webserver"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload/uploadservice"
	"github.com/Activer007/SupaShare/internal/webserver/ssl"
)

// versionGokapi is the current version in readable form.
const versionGokapi = "2.14.0"

// The following calls update the version numbers, update documentation, minify Js/CSS and build the WASM modules
//go:generate go run "../../build/go-generate/updateVersionNumbers.go"
//go:generate go run "../../build/go-generate/updateProtectedUrls.go"
//go:generate go run "../../build/go-generate/updateEnvVariables.go"
//go:generate go run "../../build/go-generate/updateApiRouting.go"
//go:generate go run "../../build/go-generate/buildWasm.go"
//go:generate go run "../../build/go-generate/copyStaticFiles.go"
//go:generate go run "../../build/go-generate/minifyStaticContent.go"

// Main routine that is called on startup
func main() {
	passedFlags := flagparser.ParseFlags()
	handleServiceInstall(passedFlags)
	handleDbMigration(passedFlags)

	showVersion(passedFlags)
	fmt.Println(logo)
	fmt.Println("SupaShare v" + versionGokapi + " starting")
	setup.RunIfFirstStart()
	configuration.Load()
	if !reconfigureServer(passedFlags) {
		configuration.ConnectDatabase()
	}

	setDeploymentPassword(passedFlags)
	checkIfUserExists()
	encryption.Init(*configuration.Get())
	authentication.Init(configuration.Get().Authentication)
	createSsl(passedFlags)
	initCloudConfig(passedFlags)
	if !recoverUploads() {
		database.Close()
		os.Exit(1)
	}
	storage.CleanUp(true)
	logging.LogStartup()
	showDeprecationWarnings()
	serverstats.Init()
	uploadservice.Start(context.Background())
	go webserver.Start()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	exitCode := 0
	if !shutdown() {
		exitCode = 1
	}
	os.Exit(exitCode)
}

func recoverUploads() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := storage.RecoverUploads(ctx, fileupload.PrepareReceiver)
	// The report only contains bounded hashes and fixed reason codes. Raw I/O
	// errors can include private names, storage keys or database connection data.
	encoded, _ := json.Marshal(report)
	fmt.Println("Upload startup recovery: " + string(encoded))
	if err != nil {
		fmt.Println("Upload recovery requires manual review; see docs/security/UPLOAD-RECOVERY-RUNBOOK.md")
		return false
	}
	return true
}

func shutdown() bool {
	fmt.Println("Shutting down...")
	if !webserver.Shutdown() {
		fmt.Println("Upload shutdown incomplete; database remains open until process exit")
		return false
	}
	serverstats.Shutdown()
	logging.LogShutdown()
	database.Close()
	return true
}

// Checks for command line arguments that have to be parsed before loading the configuration
func showVersion(passedFlags flagparser.MainFlags) {
	if !passedFlags.ShowVersion {
		return
	}
	fmt.Println("SupaShare v" + versionGokapi)
	fmt.Println()
	fmt.Println("Builder: " + environment.Builder)
	fmt.Println("Build Date: " + environment.BuildTime)
	fmt.Println("Is Docker Version: " + environment.IsDocker)
	info, ok := debug.ReadBuildInfo()
	if ok {
		fmt.Println("Go Version: " + info.GoVersion)
	} else {
		fmt.Println("Go Version: unknown")
	}
	if info == nil {
		fmt.Println("Build Settings: unknown")
	} else {
		parseBuildSettings(info.Settings)
	}
	osExit(0)
}

func showDeprecationWarnings() {
	for _, dep := range configuration.GetEnvironment().ActiveDeprecations {
		fmt.Println()
		fmt.Println("WARNING, deprecated feature: " + dep.Name)
		fmt.Println(dep.Description)
		fmt.Println("See " + dep.DocUrl + " for more information.")
		fmt.Println()
		logging.LogDeprecation(dep)
	}
}

func parseBuildSettings(infos []debug.BuildSetting) {
	lookups := make(map[string]string)
	lookups["-tags"] = "Build Tags"
	lookups["vcs.revision"] = "Git Commit"
	lookups["vcs.time"] = "Git Commit Timestamp"
	lookups["GOARCH"] = "Architecture"
	lookups["GOOS"] = "Operating System"

	for key, value := range lookups {
		result := "None"
		for _, buildSetting := range infos {
			if buildSetting.Key == key {
				result = buildSetting.Value
				break
			}
		}
		fmt.Println(value + ": " + result)
	}

	for _, info := range infos {
		if info.Key == "vcs.modified" {
			if info.Value == "true" {
				fmt.Println("Code has been modified after last git commit")
			}
			break
		}
	}
}

func initCloudConfig(passedFlags flagparser.MainFlags) {
	cConfig, ok := cloudconfig.Load()
	if ok && aws.Init(cConfig.Aws) {
		fmt.Println("Saving new files to cloud storage")
		filesystem.SetAws()
		encLevel := configuration.Get().Encryption.Level
		env := environment.New()
		corsCheckDisabled := passedFlags.DisableCorsCheck || env.DisableCorsCheck
		if !corsCheckDisabled && (encLevel == encryption.FullEncryptionStored || encLevel == encryption.FullEncryptionInput || encLevel == encryption.EndToEndEncryption) {
			ok, err := aws.IsCorsCorrectlySet(cConfig.Aws.Bucket, configuration.Get().ServerUrl)
			if err != nil {
				fmt.Println("Warning: Cannot check CORS settings. " + err.Error())
				fmt.Println("If your provider does not implement the CORS API call and you are certain that it is set correctly, you can disable this check with --disable-cors-check")
			} else {
				if !ok {
					fmt.Println("Warning: CORS settings for bucket " + cConfig.Aws.Bucket + " might not be set correctly. Download might not be possible with encryption.")
				}
			}
		}
	} else {
		fmt.Println("Saving new files to local storage")
	}
}

// Checks for command line arguments that have to be parsed after loading the configuration
func reconfigureServer(passedFlags flagparser.MainFlags) bool {
	if passedFlags.Reconfigure {
		logging.LogSetup()
		setup.RunConfigModification()
		return true
	}
	return false
}

func createSsl(passedFlags flagparser.MainFlags) {
	if passedFlags.CreateSsl {
		ssl.GenerateIfInvalidCert(configuration.Get().ServerUrl, true)
	}
}

func checkIfUserExists() {
	if len(database.GetAllUsers()) == 0 {
		fmt.Println("No user found in database. Please run setup first or create user with --deployment-password")
		os.Exit(1)
	}
}

func handleDbMigration(passedFlags flagparser.MainFlags) {
	if !passedFlags.Migration.DoMigration {
		return
	}
	migration.Do(passedFlags.Migration)
	osExit(0)
}

func handleServiceInstall(passedFlags flagparser.MainFlags) {
	if passedFlags.InstallService && passedFlags.UninstallService {
		fmt.Println("Error: Both install and uninstall flags are set.")
		osExit(1)
	}
	if passedFlags.InstallService {
		systemd.InstallService()
		osExit(0)
	}
	if passedFlags.UninstallService {
		systemd.UninstallService()
		osExit(0)
	}
}

func setDeploymentPassword(passedFlags flagparser.MainFlags) {
	if passedFlags.DeploymentPassword == "" {
		return
	}
	logging.LogDeploymentPassword()
	configuration.SetDeploymentPassword(passedFlags.DeploymentPassword)
}

var osExit = os.Exit

// ASCII art logo
const logo = `
██████   ██████   ██   ██  █████  ██████  ██ 
██       ██    ██ ██  ██  ██   ██ ██   ██ ██ 
██   ███ ██    ██ █████   ███████ ██████  ██ 
██    ██ ██    ██ ██  ██  ██   ██ ██      ██ 
 ██████   ██████  ██   ██ ██   ██ ██      ██ 
                                             `
