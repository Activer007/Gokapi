package webserver

/**
Handling of webserver and requests / uploads
*/

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	templatetext "text/template"
	"time"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/i18n"
	"github.com/Activer007/SupaShare/internal/logging"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filerequest"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/webserver/api"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/downloadPasswordToken"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/oauth"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/sessionmanager"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/tokengeneration"
	"github.com/Activer007/SupaShare/internal/webserver/browseraccess"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling"
	"github.com/Activer007/SupaShare/internal/webserver/favicon"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload/uploadservice"
	"github.com/Activer007/SupaShare/internal/webserver/ratelimiter"
	"github.com/Activer007/SupaShare/internal/webserver/sse"
	"github.com/Activer007/SupaShare/internal/webserver/ssl"
	"github.com/NYTimes/gziphandler"
)

// TODO add 404 handler

// staticFolderEmbedded is the embedded version of the "static" folder
// This contains JS files, CSS, images etc
//
//go:embed web/static
var staticFolderEmbedded embed.FS

// templateFolderEmbedded is the embedded version of the "templates" folder
// This contains templates that SupaShare uses for creating the HTML output
//
//go:embed web/templates
var templateFolderEmbedded embed.FS

// wasmDownloadFile is the compiled binary of the wasm downloader
// Will be generated with go generate ./...
//
//go:embed web/main.wasm
var wasmDownloadFile embed.FS

// wasmE2EFile is the compiled binary of the wasm e2e encrypter
// Will be generated with go generate ./...
//
//go:embed web/e2e.wasm
var wasmE2EFile embed.FS

const timeOutWebserverRead = 2 * time.Hour
const timeOutWebserverWrite = 12 * time.Hour

// templateFolder contains all parsed templates
var templateFolder *template.Template

// customStaticInfo is passed to all templates, so custom CSS or JS can be embedded
var customStaticInfo customStatic

// imageExpiredPicture is sent for an expired hotlink
var imageExpiredPicture []byte

// srv is the web server that is used for this module
var srvMu sync.Mutex
var srv *http.Server

// Start the webserver on the port set in the config
func Start() {
	workers := uploadservice.Current()
	initTemplates(templateFolderEmbedded)
	webserverDir, _ := fs.Sub(staticFolderEmbedded, "web/static")
	var err error

	mux := http.NewServeMux()
	loadCustomCssJsInfo(webserverDir)
	loadExpiryImage()

	mux.Handle("/", filesystemHandler(webserverDir))
	mux.HandleFunc("/auth/token", requireLogin(handleGenerateAuthToken, false, false))
	mux.HandleFunc("/admin", requireLogin(showAdminMenu, true, false))
	mux.HandleFunc("/api/", processApi)
	mux.HandleFunc("/apiKeys", requireLogin(showApiAdmin, true, false))
	mux.HandleFunc("/changePassword", requireLogin(changePassword, true, true))
	mux.HandleFunc("/d", showDownload)
	mux.HandleFunc("/delivery/{token}", showDelivery)
	mux.HandleFunc("/downloadFile", downloadFile)
	mux.HandleFunc("/downloadPresigned", requireLogin(downloadPresigned, false, false))
	mux.HandleFunc("/e2eSetup", requireLogin(showE2ESetup, true, false))
	mux.HandleFunc("/error", showError)
	mux.HandleFunc("/filerequests", requireLogin(showUploadRequest, true, false))
	mux.HandleFunc("/forgotpw", forgotPassword)
	mux.HandleFunc("/h/", showHotlink)
	mux.HandleFunc("/hotlink/", showHotlink) // backward compatibility
	mux.HandleFunc("/index", showIndex)
	mux.HandleFunc("/login", showLogin)
	mux.HandleFunc("/logs", requireLogin(showLogs, true, false))
	mux.HandleFunc("/logout", doLogout)
	mux.HandleFunc("/publicUpload", showPublicUpload)
	mux.HandleFunc("/r/{token}", showPublicUpload)
	mux.HandleFunc("/uploadChunk", requireLogin(uploadChunk, false, false))
	mux.HandleFunc("/uploadStatus", requireLogin(sse.GetStatusSSE, false, false))
	mux.HandleFunc("/users", requireLogin(showUserAdmin, true, false))
	mux.Handle("/main.wasm", gziphandler.GzipHandler(http.HandlerFunc(serveDownloadWasm)))
	mux.Handle("/e2e.wasm", gziphandler.GzipHandler(http.HandlerFunc(serveE2EWasm)))
	mux.HandleFunc("/d/{id}/{filename}", redirectFromFilename)
	mux.HandleFunc("/dh/{id}/{filename}", downloadFileWithNameInUrl)

	addMuxForCustomContent(mux)

	if configuration.Get().Authentication.Method == models.AuthenticationOAuth2 {
		oauth.Init(configuration.Get().ServerUrl, configuration.Get().Authentication)
		mux.HandleFunc("/oauth-login", oauth.HandlerLogin)
		mux.HandleFunc("/oauth-callback", oauth.HandlerCallback)
	}

	fmt.Println("Binding webserver to " + configuration.Get().Port)
	defaultLanguage := "en"
	if configuration.Get().UI != nil {
		defaultLanguage = configuration.Get().UI.DefaultLanguage
	}
	server := &http.Server{
		Addr:              configuration.Get().Port,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
		ReadTimeout:       timeOutWebserverRead,
		WriteTimeout:      timeOutWebserverWrite,
		Handler:           withResponsePolicy(browseraccess.DesktopOnly(withLanguagePreference(withManagementBudget(mux, managementBudget)), defaultLanguage)),
		BaseContext:       func(net.Listener) context.Context { return workers.Context() },
	}
	srvMu.Lock()
	if workers.Context().Err() != nil {
		srvMu.Unlock()
		return
	}
	srv = server
	srvMu.Unlock()
	infoMessage := "Webserver can be accessed at " + configuration.Get().ServerUrl + "admin\nPress CTRL+C to stop SupaShare"
	if strings.Contains(configuration.Get().ServerUrl, "127.0.0.1") {
		if configuration.Get().UseSsl {
			infoMessage = strings.Replace(infoMessage, "http://", "https://", 1)
		} else {
			infoMessage = strings.Replace(infoMessage, "https://", "http://", 1)
		}
	}
	if configuration.Get().UseSsl {
		ssl.GenerateIfInvalidCert(configuration.Get().ServerUrl, false)
		fmt.Println(infoMessage)
		err = server.ListenAndServeTLS(ssl.GetCertificateLocations())
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	} else {
		fmt.Println(infoMessage)
		err = server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func filesystemHandler(webserverDir fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/favicon") {
			handleFavicon(w, r)
			return
		}
		addCacheHeader(w)
		http.FileServer(http.FS(webserverDir)).ServeHTTP(w, r)
	}
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	icon := favicon.GetFavicon(r.URL.Path)
	_, _ = w.Write(icon)
}

func loadExpiryImage() {
	svgTemplate, err := templatetext.ParseFS(templateFolderEmbedded, "web/templates/expired_file_svg.tmpl")
	helper.Check(err)
	var buf bytes.Buffer
	language := "en"
	if configuration.Get().UI != nil {
		language = configuration.Get().UI.DefaultLanguage
	}
	err = svgTemplate.Execute(&buf, struct {
		PublicName string
		Message    string
	}{PublicName: configuration.Get().PublicName, Message: i18n.New(language).T("file.expired")})
	helper.Check(err)
	imageExpiredPicture = buf.Bytes()
}

// Shutdown reports whether HTTP handlers and admitted upload workers drained.
// It does not assert that every legacy timer or unrelated goroutine has exited.
func Shutdown() bool {
	workers := uploadservice.Current()
	workers.BeginShutdown()
	sse.Shutdown()
	srvMu.Lock()
	server := srv
	srvMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return shutdownUploads(ctx, server, workers)
}

func shutdownUploads(ctx context.Context, server *http.Server, workers *uploadservice.Generation) bool {
	if ctx == nil || workers == nil {
		return false
	}
	workers.BeginShutdown()
	var httpErr error
	if server != nil {
		httpErr = server.Shutdown(ctx)
	}
	if httpErr != nil {
		log.Print("HTTP upload drain incomplete; durable reservations retained")
	}
	workerErr := workers.Wait(ctx)
	if workerErr != nil {
		log.Print("Upload worker drain incomplete; durable reservations retained")
	}
	return httpErr == nil && workerErr == nil
}

// Initialises the templateFolder variable by scanning through all the templates.
// If a folder "templates" exists in the main directory, it is used.
// Otherwise, templateFolderEmbedded will be used.
func initTemplates(templateFolderEmbedded embed.FS) {
	var err error

	funcMap := template.FuncMap{
		"newAdminButtonContext": newAdminButtonContext,
		"newFileRequestContext": newFileRequestContext,
	}
	if helper.FolderExists("templates") {
		fmt.Println("Found folder 'templates', using local folder instead of internal template folder")
		templateFolder, err = template.New("").Funcs(funcMap).ParseGlob("templates/*.tmpl")
		helper.Check(err)
	} else {
		templateFolder, err = template.New("").Funcs(funcMap).ParseFS(templateFolderEmbedded, "web/templates/*.tmpl")
		helper.Check(err)
	}
}

// Sends a redirect HTTP output to the client. Variable url is used to redirect to ./url
func redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func redirectOnIncorrectId(w http.ResponseWriter, r *http.Request, url string) {
	if err := ratelimiter.WaitOnFailedId(r); err != nil {
		return
	}
	redirect(w, r, url)
}

type redirectValues struct {
	pageView
	FileId           string
	RedirectUrl      string
	Name             string
	Size             string
	PublicName       string
	BaseUrl          string
	PasswordRequired bool
}

// Handling of /id/?/? - used when filename shall be displayed, will redirect to the regular download URL
func redirectFromFilename(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	id := r.PathValue("id")
	file, ok := storage.GetFile(id)
	if !ok {
		redirect(w, r, "../../error")
		return
	}

	config := configuration.Get()
	err := renderPage(w, r, "redirect_filename", &redirectValues{
		FileId:           id,
		RedirectUrl:      "d",
		Name:             file.Name,
		Size:             file.Size,
		PublicName:       config.PublicName,
		BaseUrl:          config.ServerUrl,
		PasswordRequired: file.PasswordHash != ""})
	helper.CheckIgnoreTimeout(err)
}

// Handling of /main.wasm
func serveDownloadWasm(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "public, max-age=100800") // 2 days
	w.Header().Set("Content-Type", "application/wasm")
	file, err := wasmDownloadFile.ReadFile("web/main.wasm")
	helper.Check(err)
	_, _ = w.Write(file)
}

// Handling of /e2e.wasm
func serveE2EWasm(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "public, max-age=100800") // 2 days
	w.Header().Set("Content-Type", "application/wasm")
	file, err := wasmE2EFile.ReadFile("web/e2e.wasm")
	helper.Check(err)
	_, _ = w.Write(file)
}

// Handling of /logout
func doLogout(w http.ResponseWriter, r *http.Request) {
	authentication.Logout(w, r)
}

// Handling of /index and redirecting to globalConfig.RedirectUrl
func showIndex(w http.ResponseWriter, r *http.Request) {
	err := renderPage(w, r, "index", &genericView{RedirectUrl: configuration.Get().RedirectUrl,
		PublicName:    configuration.Get().PublicName,
		CustomContent: customStaticInfo})
	helper.CheckIgnoreTimeout(err)
}

func handleGenerateAuthToken(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	permString := r.Header.Get("permission")
	permission, err := models.ApiPermissionFromString(permString)
	if err != nil {
		http.Error(w, "Invalid permission", http.StatusBadRequest)
		return
	}
	token, expiry, err := tokengeneration.Generate(user, permission)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	_, _ = w.Write([]byte("{\"key\":\"" + token + "\",\"expiry\":" + strconv.FormatInt(expiry, 10) + "}"))
}

// Handling of /changePassword
func changePassword(w http.ResponseWriter, r *http.Request) {
	var errMessage string
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	if !user.ResetPassword {
		redirect(w, r, "admin")
		return
	}
	err = r.ParseForm()
	if err != nil {
		fmt.Println("Invalid form data sent to server for /changePassword")
		fmt.Println(err)
		errMessage = "password.incomplete"
	} else {
		var ok bool
		var pwHash string

		pw := r.PostForm.Get("newpw")
		csrf := r.PostForm.Get("csrf-token")
		validCsrf := pw != "" && csrftoken.IsValidBound(csrftoken.TypePasswordChange, csrf, browserCSRFBinding(r))
		pwHash, ok, err = validateNewPassword(pw, user, validCsrf)
		if err != nil {
			errMessage = err.Error()
		}
		if ok {
			user.Password = pwHash
			user.ResetPassword = false
			database.SaveUser(user, false)
			database.DeleteAllSessionsByUser(user.Id)
			sessionmanager.LogoutSession(w, r)
			redirect(w, r, "login")
			return
		}
	}
	config := configuration.Get()
	err = renderPage(w, r, "changepw",
		&genericView{PublicName: config.PublicName,
			MinPasswordLength: configuration.GetEnvironment().MinLengthPassword,
			ErrorMessage:      errMessage,
			CustomContent:     customStaticInfo,
			CsrfToken:         newBrowserCSRF(w, r, csrftoken.TypePasswordChange)})
	helper.CheckIgnoreTimeout(err)
}

// validateNewPassword validates the new password and returns the new password hash if the password is valid.
// If the password is not valid, it returns an error message and an empty string.
// If the password is valid, it returns the hash as a string and true.
func validateNewPassword(newPassword string, user models.User, validCsrf bool) (string, bool, error) {
	if len(newPassword) == 0 {
		return user.Password, false, nil
	}
	if !validCsrf {
		return "", false, errors.New("password.incomplete")
	}
	if len(newPassword) < configuration.GetEnvironment().MinLengthPassword {
		return "", false, errors.New("password.tooShort")
	}
	isSame, _ := configuration.VerifyPassword(newPassword, user.Password, "")
	if isSame {
		return "", false, errors.New("password.unchanged")
	}
	newPasswordHash := configuration.HashPassword(newPassword, false, "")
	return newPasswordHash, true, nil
}

func firstLetterUpper(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}

// Handling of /error
func showError(w http.ResponseWriter, r *http.Request) {

	displayedError := errorHandling.Get(r)

	if r.URL.Query().Has("e2e") {
		displayedError.ErrorId = errorHandling.TypeE2ECipher
		displayedError.IsGeneric = true
		displayedError.CardWidth = "25rem"
	}

	err := renderPage(w, r, "error", &genericView{
		ErrorId:           displayedError.ErrorId,
		ErrorCardWidth:    displayedError.CardWidth,
		IsGenericError:    displayedError.IsGeneric,
		ErrorTitle:        displayedError.Title,
		ErrorMessage:      displayedError.Message,
		ErrorOauthMessage: displayedError.OAuthProviderMessage,
		PublicName:        configuration.Get().PublicName,
		CustomContent:     customStaticInfo})
	helper.CheckIgnoreTimeout(err)
}

// Handling of /forgotpw
func forgotPassword(w http.ResponseWriter, r *http.Request) {
	err := renderPage(w, r, "forgotpw", &genericView{
		PublicName:    configuration.Get().PublicName,
		CustomContent: customStaticInfo})
	helper.CheckIgnoreTimeout(err)
}

// Handling of /filerequest
func showUploadRequest(w http.ResponseWriter, r *http.Request) {
	userId, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	view := (&AdminView{}).convertGlobalConfig(ViewFileRequests, userId)

	if !authorization.Authorize(view.ActiveUser, authorization.ManageFileRequests, 0) {
		redirect(w, r, "admin")
		return
	}
	err = renderPage(w, r, "uploadreq", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /api
// If the user is authenticated, this menu lists all uploads and enables uploading new files
func showApiAdmin(w http.ResponseWriter, r *http.Request) {
	userId, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	view := (&AdminView{}).convertGlobalConfig(ViewAPI, userId)

	if configuration.GetEnvironment().DisableApiMenu && !view.ActiveUser.IsAdmin() {
		redirect(w, r, "admin")
		return
	}

	err = renderPage(w, r, "api", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /users
// If user is authenticated, this menu lists all users
func showUserAdmin(w http.ResponseWriter, r *http.Request) {
	userId, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	view := (&AdminView{}).convertGlobalConfig(ViewUsers, userId)
	if !authorization.Authorize(view.ActiveUser, authorization.ManageUsers, 0) || configuration.Get().Authentication.Method == models.AuthenticationDisabled {
		redirect(w, r, "admin")
		return
	}
	err = renderPage(w, r, "users", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /api/
func processApi(w http.ResponseWriter, r *http.Request) {
	api.Process(w, r)
}

// Handling of /login
// Shows a login form. If not authenticated, client needs to wait for three seconds.
// If correct, a new session is created and the user is redirected to the admin menu
func showLogin(w http.ResponseWriter, r *http.Request) {
	_, ok, err := authentication.IsAuthenticated(w, r)
	if err != nil {
		errorHandling.RedirectToErrorPage(w, r, "error.login.title", err.Error(), errorHandling.WidthDefault)
		return
	}
	if ok {
		redirect(w, r, "admin")
		return
	}
	if configuration.Get().Authentication.Method == models.AuthenticationHeader {
		errorHandling.RedirectToErrorPage(w, r, "error.unauthorized.title",
			"error.header.help", errorHandling.WidthDefault)
		return
	}
	if configuration.Get().Authentication.Method == models.AuthenticationOAuth2 {
		// If user clicked logout, force consent
		if r.URL.Query().Has("consent") {
			redirect(w, r, "oauth-login?consent=true")
		} else {
			redirect(w, r, "oauth-login")
		}
		return
	}
	err = r.ParseForm()
	if err != nil {
		fmt.Println("Invalid form data sent to server for /login")
		fmt.Println(err)
		return
	}
	user := r.PostForm.Get("username")
	pw := r.PostForm.Get("password")
	csfr := r.PostForm.Get("csrf-token")
	failedLogin := false
	failedCsrf := false
	if pw != "" && user != "" {
		ip := logging.GetIpAddress(r)
		if err := ratelimiter.WaitOnLogin(r.Context(), ip); err != nil {
			return
		}
		retrievedUser, validCredentials, validCsfr := authentication.IsCorrectUsernameAndPasswordForBrowser(user, pw, csfr, browserCSRFBinding(r))
		if validCredentials {
			logging.LogValidLogin(user)
			sessionmanager.CreateSession(w, false, 0, retrievedUser.Id)
			redirect(w, r, "admin")
			return
		}
		if validCsfr {
			logging.LogInvalidLogin(user, ip)
		}
		failedCsrf = !validCsfr
		failedLogin = true
	}
	err = renderPage(w, r, "login", &LoginView{
		IsFailedLogin: failedLogin,
		IsFailedCsfr:  failedCsrf,
		User:          user,
		IsAdminView:   false,
		PublicName:    configuration.Get().PublicName,
		CustomContent: customStaticInfo,
		CsrfToken:     newBrowserCSRF(w, r, csrftoken.TypeLogin),
	})
	helper.CheckIgnoreTimeout(err)
}

// LoginView contains variables for the login template
type LoginView struct {
	pageView
	IsFailedLogin  bool
	IsFailedCsfr   bool
	IsAdminView    bool
	IsDownloadView bool
	User           string
	PublicName     string
	CsrfToken      string
	CustomContent  customStatic
}

// Handling of /d
// Checks if a file exists for the submitted ID
// If it exists, a download form is shown, or a password needs to be entered.
func showDownload(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	keyId := queryUrl(w, r, "id", errorHandling.TypeFileNotFound)
	file, ok := storage.GetFile(keyId)
	if !ok || file.IsFileRequest() {
		redirectOnIncorrectId(w, r, "error")
		return
	}

	config := configuration.Get()

	view := &DownloadView{
		Name:               file.Name,
		Size:               file.Size,
		UploadDate:         file.UploadDate,
		Id:                 file.Id,
		IsDownloadView:     true,
		EndToEndEncryption: file.Encryption.IsEndToEndEncrypted,
		PublicName:         config.PublicName,
		BaseUrl:            config.ServerUrl,
		IsFailedLogin:      false,
		UsesHttps:          configuration.UsesHttps(),
		CustomContent:      customStaticInfo,
	}

	if file.RequiresClientDecryption() {
		view.ClientSideDecryption = true
		if !file.Encryption.IsEndToEndEncrypted {
			cipher, err := encryption.GetCipherFromFile(file.Encryption)
			helper.Check(err)
			view.Cipher = base64.StdEncoding.EncodeToString(cipher)
		}
	}

	if file.PasswordHash != "" && !isValidPwCookie(r, file) {
		_ = r.ParseForm()
		enteredPassword := r.PostForm.Get("password")
		if enteredPassword == "" {
			view.IsPasswordView = true
			err := renderPage(w, r, "download_password", view)
			helper.CheckIgnoreTimeout(err)
			return
		}

		ip := logging.GetIpAddress(r)
		if err := ratelimiter.WaitOnDownloadPassword(r.Context(), ip); err != nil {
			return
		}

		isValid, isLegacy := configuration.VerifyPassword(enteredPassword, file.PasswordHash, configuration.Get().Authentication.SaltFiles)
		if isValid {
			// Migrate legacy passwords to the new format
			// Will be removed in the future
			if isLegacy {
				newHash := configuration.HashPassword(enteredPassword, false, "")
				updated, err := storage.UpgradeFilePassword(r.Context(), file.Id, file.PasswordHash, newHash)
				if err != nil || !updated {
					http.Error(w, "File state changed; retry the download", http.StatusConflict)
					return
				}
				file.PasswordHash = newHash
			}
			writeFilePwCookie(w, file)
			// redirect so that there is no post data to be resent if user refreshes page
			redirect(w, r, "d?id="+file.Id)
			return
		}
		view.IsFailedLogin = true
		view.IsPasswordView = true
		err := renderPage(w, r, "download_password", view)
		helper.CheckIgnoreTimeout(err)
		return
	}

	err := renderPage(w, r, "download", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /h/ and /hotlink/
// Hotlinks an image or returns a static error image if image has expired
func showHotlink(w http.ResponseWriter, r *http.Request) {
	hotlinkId := strings.Replace(r.URL.Path, "/hotlink/", "", 1)
	hotlinkId = strings.Replace(hotlinkId, "/h/", "", 1)
	addNoCacheHeader(w)
	file, ok := storage.GetFileByHotlink(hotlinkId)
	if !ok || file.IsFileRequest() {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(imageExpiredPicture)
		return
	}
	validFile := storage.ServeFile(file, w, r, false, true, false, true)
	if !validFile {
		// Only called if the file has already expired during the expiry check of storage.ServeFile()
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(imageExpiredPicture)
		return
	}
}

// Checks if a file is associated with the GET parameter from the current URL
func queryUrl(w http.ResponseWriter, r *http.Request, keyword string, errorType int) string {
	keys, ok := r.URL.Query()[keyword]
	if !ok || len(keys[0]) < environment.MinLengthId {
		errorHandling.RedirectGenericErrorPage(w, r, errorType)
		return ""
	}
	return keys[0]
}

// Handling of /admin
// If user is authenticated, this menu lists all uploads and enables uploading new files
func showAdminMenu(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}

	config := configuration.Get()
	if config.Encryption.Level == encryption.EndToEndEncryption {
		e2einfo := database.GetEnd2EndInfo(user.Id)
		if !e2einfo.HasBeenSetUp() {
			redirect(w, r, "e2eSetup")
			return
		}
	}

	view := (&AdminView{}).convertGlobalConfig(ViewMain, user)
	if len(configuration.GetEnvironment().ActiveDeprecations) > 0 {
		if user.IsSuperAdmin() {
			view.ShowDeprecationNotice = true
		}
	}

	err = renderPage(w, r, "admin", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /logs
// If user is authenticated, this menu shows the stored logs
func showLogs(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	view := (&AdminView{}).convertGlobalConfig(ViewLogs, user)
	if !authorization.Authorize(view.ActiveUser, authorization.ManageLogs, 0) {
		redirect(w, r, "admin")
		return
	}
	err = renderPage(w, r, "logs", view)
	helper.CheckIgnoreTimeout(err)
}

func showE2ESetup(w http.ResponseWriter, r *http.Request) {
	if configuration.Get().Encryption.Level != encryption.EndToEndEncryption {
		redirect(w, r, "admin")
		return
	}

	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		panic(err)
	}
	e2einfo := database.GetEnd2EndInfo(user.Id)
	err = renderPage(w, r, "e2esetup", &e2ESetupView{
		HasBeenSetup:  e2einfo.HasBeenSetUp(),
		PublicName:    configuration.Get().PublicName,
		CustomContent: customStaticInfo})
	helper.CheckIgnoreTimeout(err)
}

// DownloadView contains parameters for the download template
type DownloadView struct {
	pageView
	Name                 string
	Size                 string
	UploadDate           int64
	Id                   string
	Cipher               string
	PublicName           string
	BaseUrl              string
	IsFailedLogin        bool
	IsAdminView          bool
	IsDownloadView       bool
	IsPasswordView       bool
	ClientSideDecryption bool
	EndToEndEncryption   bool
	UsesHttps            bool
	CustomContent        customStatic
}

type e2ESetupView struct {
	pageView
	IsAdminView    bool
	IsDownloadView bool
	HasBeenSetup   bool
	PublicName     string
	CustomContent  customStatic
}

// AdminView contains parameters for all admin-related pages
type AdminView struct {
	pageView
	Items                 []models.FileApiOutput
	ApiKeys               []models.ApiKey
	Users                 []userInfo
	FileRequests          []models.FileRequest
	ActiveUser            models.User
	UserMap               map[int]*models.User
	ServerUrl             string
	PublicName            string
	IsAdminView           bool
	IsDownloadView        bool
	IsApiView             bool
	IsLogoutAvailable     bool
	IsUserTabAvailable    bool
	EndToEndEncryption    bool
	IncludeFilename       bool
	IsInternalAuth        bool
	ShowApiMenu           bool
	ShowDeprecationNotice bool
	MaxFileSize           int
	ActiveView            int
	ChunkSize             int
	MaxParallelUploads    int
	MinLengthPassword     int
	FileRequestMaxFiles   int
	FileRequestMaxSize    int
	TotalFiles            int
	CpuLoad               int
	MemoryUsagePercent    int
	DiskUsagePercent      int
	DataServed            int64
	Uptime                int64
	TimeNow               int64
	TrafficSince          int64
	MemoryUsage           uint64
	MemoryTotal           uint64
	DiskUsage             uint64
	DiskTotal             uint64
	TotalTraffic          uint64

	CustomContent customStatic
}

// getUserMap needs to return the map with pointers; otherwise template cannot call
// functions associated with it
func getUserMap() map[int]*models.User {
	result := make(map[int]*models.User)
	users := database.GetAllUsers()
	for _, user := range users {
		result[user.Id] = &user
	}
	return result
}

const (
	// ViewMain is the identifier for the main menu
	ViewMain = iota
	// ViewLogs is the identifier for the log viewer menu
	ViewLogs
	// ViewAPI is the identifier for the API menu
	ViewAPI
	// ViewUsers is the identifier for the user management menu
	ViewUsers
	// ViewFileRequests is the identifier for the file request menu
	ViewFileRequests
)

// Converts the globalConfig variable to an AdminView struct to pass the infos to
// the admin template
func (u *AdminView) convertGlobalConfig(view int, user models.User) *AdminView {
	var metaDataList []models.FileApiOutput
	var apiKeyList []models.ApiKey

	config := configuration.Get()
	u.IsInternalAuth = config.Authentication.Method == models.AuthenticationInternal
	u.ActiveUser = user
	u.UserMap = getUserMap()
	u.CustomContent = customStaticInfo
	switch view {
	case ViewMain:
		for _, element := range database.GetAllMetadata() {
			if !authorization.Authorize(user, authorization.ReadFile, element.UserId) {
				continue
			}
			fileInfo, err := element.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
			helper.Check(err)
			metaDataList = append(metaDataList, fileInfo)
		}
		metaDataList = sortMetaDataApi(metaDataList)
	case ViewAPI:
		for _, apiKey := range database.GetAllApiKeys() {
			// Double-checking if the owner of the API key exists
			// If the user was manually deleted from the database, this could lead to a crash
			// in the API view
			_, ok := u.UserMap[apiKey.UserId]
			if !ok {
				continue
			}
			if !apiKey.IsSystemKey && !apiKey.IsUploadRequestKey() {
				if authorization.Authorize(user, authorization.ManageAPIKeys, apiKey.UserId) {
					apiKeyList = append(apiKeyList, apiKey)
				}
			}
		}
		apiKeyList = sortApiKeys(apiKeyList)
	case ViewLogs:
		u.TotalFiles = serverstats.GetTotalFiles()
		u.Uptime = serverstats.GetUptime()
		u.TotalTraffic, u.TrafficSince = serverstats.GetCurrentTraffic()
		_, u.MemoryUsage, u.MemoryTotal, u.MemoryUsagePercent = serverstats.GetMemoryInfo()
		_, u.DiskUsage, u.DiskTotal, u.DiskUsagePercent = serverstats.GetDiskInfo()
		u.CpuLoad = serverstats.GetCpuUsage()
	case ViewUsers:
		uploadCounts := storage.GetUploadCounts()
		u.Users = make([]userInfo, 0)
		for _, userEntry := range database.GetAllUsers() {
			userWithUploads := userInfo{
				UploadCount: uploadCounts[userEntry.Id],
				User:        userEntry,
			}
			// Otherwise the user is not shown as online, if /users is opened as first page
			if userEntry.Id == user.Id {
				userWithUploads.User.LastOnline = time.Now().Unix()
			}
			u.Users = append(u.Users, userWithUploads)
		}
	case ViewFileRequests:
		// Creation limits depend on the viewer, including an empty first visit.
		if !user.IsAdmin() {
			u.FileRequestMaxFiles = configuration.GetEnvironment().MaxFilesGuestUpload
			u.FileRequestMaxSize = configuration.GetEnvironment().MaxSizeGuestUploadMb
		}
		for _, fileRequest := range filerequest.GetAll() {
			// Double-checking if the owner of the file request exists
			// If the user was manually deleted from the database, this could lead to a crash
			// in the file request view
			_, ok := u.UserMap[fileRequest.UserId]
			if !ok {
				continue
			}
			if !authorization.Authorize(user, authorization.ReadFile, fileRequest.UserId) {
				continue
			}
			fileRequest.Files = sortMetaData(fileRequest.Files)
			u.FileRequests = append(u.FileRequests, fileRequest)
		}
	}

	showApiMenu := true
	if configuration.GetEnvironment().DisableApiMenu {
		showApiMenu = user.IsAdmin()
	}

	u.ServerUrl = config.ServerUrl
	u.Items = metaDataList
	u.PublicName = config.PublicName
	u.ApiKeys = apiKeyList
	u.TimeNow = time.Now().Unix()
	u.IsAdminView = true
	u.ActiveView = view
	u.MaxFileSize = config.MaxFileSizeMB
	u.IsLogoutAvailable = authentication.IsLogoutAvailable()
	u.ShowApiMenu = showApiMenu
	u.IsUserTabAvailable = config.Authentication.Method != models.AuthenticationDisabled
	u.EndToEndEncryption = config.Encryption.Level == encryption.EndToEndEncryption
	u.MaxParallelUploads = config.MaxParallelUploads
	u.ChunkSize = config.ChunkSize
	u.IncludeFilename = config.IncludeFilename
	return u
}

// sortMetaDataApi arranges the provided array so that Fies are sorted by the most recent upload first and if that is equal,
// then by most time remaining first. If that is equal, then sort by ID.
func sortMetaDataApi(input []models.FileApiOutput) []models.FileApiOutput {
	sort.Slice(input[:], func(i, j int) bool {
		if input[i].UploadDate != input[j].UploadDate {
			return input[i].UploadDate > input[j].UploadDate
		}
		if input[i].ExpireAt != input[j].ExpireAt {
			return input[i].ExpireAt > input[j].ExpireAt
		}
		return input[i].Id > input[j].Id
	})
	return input
}

// sortMetaData arranges the provided array so that Fies are sorted by the most recent upload first then sort by ID.
// Currently only used for the files of File Requests, all others use sortMetaDataApi
func sortMetaData(input []models.File) []models.File {
	sort.Slice(input[:], func(i, j int) bool {
		if input[i].UploadDate != input[j].UploadDate {
			return input[i].UploadDate > input[j].UploadDate
		}
		return input[i].Id > input[j].Id
	})
	return input
}

// sortApiKeys arranges the provided array so that API keys are sorted by most recent usage first and if that is equal
// then by ID
func sortApiKeys(input []models.ApiKey) []models.ApiKey {
	sort.Slice(input[:], func(i, j int) bool {
		if input[i].LastUsed != input[j].LastUsed {
			return input[i].LastUsed > input[j].LastUsed
		}
		return input[i].Id < input[j].Id
	})
	return input
}

type userInfo struct {
	UploadCount int
	User        models.User
}

// Handling of /publicUpload
func showPublicUpload(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	request, ok := publicUploadRequest(r)
	if !ok {
		if r.PathValue("token") == "" {
			errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeInvalidFileRequest)
		} else {
			// Render here so the error route and its assets remain correct under /r/.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			addPageResponsePolicy(w, "error")
			w.WriteHeader(http.StatusNotFound)
			view := &genericView{IsGenericError: true, ErrorId: errorHandling.TypeInvalidFileRequest,
				ErrorCardWidth: errorHandling.WidthWide, PublicName: configuration.Get().PublicName, CustomContent: customStaticInfo}
			view.BaseHref = publicUploadBasePath()
			helper.CheckIgnoreTimeout(renderPage(w, r, "error", view))
		}
		return
	}

	config := configuration.Get()

	view := &publicUploadView{
		PublicName:    config.PublicName,
		ChunkSize:     config.ChunkSize,
		MaxServerSize: config.MaxFileSizeMB,
		FileRequest:   &request,
		CustomContent: customStaticInfo,
	}
	view.BaseHref = publicUploadBasePath()

	err := renderPage(w, r, "publicUpload", view)
	helper.CheckIgnoreTimeout(err)
}

// Handling of /uploadChunk
// If the user is authenticated, this parses the uploaded chunk and stores it
func uploadChunk(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	maxUpload, err := fileupload.MaxUploadBytes(configuration.Get().MaxFileSizeMB)
	if err != nil {
		responseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	errCode, err := fileupload.ProcessNewChunk(w, r, false, "", user.Id, maxUpload)
	if err != nil {
		status := fileupload.ReceiveErrorStatus(err)
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(status)
		body, _ := json.Marshal(struct {
			Result       string `json:"Result"`
			ErrorMessage string `json:"ErrorMessage"`
		}{"error", err.Error()})
		_, _ = w.Write(body)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf("Upload rejected (code=%d)", errCode)
		}
		return
	}
}

// Outputs an error in json format if err!=nil
func responseError(w http.ResponseWriter, err error) {
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "{\"Result\":\"error\",\"ErrorMessage\":\""+err.Error()+"\"}")
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Println(err)
		}
	}
}

// Handling of /dh/?/?
// Hotlinks a file and has the filename in the URL
func downloadFileWithNameInUrl(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	serveFile(id, false, w, r)
}

// Handling of /downloadFile
// Outputs the file to the user and reduces the download remaining count for the file
func downloadFile(w http.ResponseWriter, r *http.Request) {
	id := queryUrl(w, r, "id", errorHandling.TypeFileNotFound)
	serveFile(id, true, w, r)
}

// Handling of /downloadPresigned
// Outputs the file to the user and reduces the download remaining count for the file, if requested
func downloadPresigned(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	presignKey, ok := r.URL.Query()["key"]
	if !ok {
		responseError(w, storage.ErrorInvalidPresign)
		return
	}
	presignedUrl, ok := presign.Get(presignKey[0])
	if !ok {
		if tryCollectionRetry(w, r, presignKey[0]) {
			return
		}
		responseError(w, storage.ErrorInvalidPresign)
		return
	}
	consumer, err := authentication.GetUserFromRequest(r)
	if err != nil {
		responseError(w, storage.ErrorInvalidPresign)
		return
	}
	consumer, consumerExists := database.GetUser(consumer.Id)
	issuer, issuerValid := api.GetDownloadUser(presignedUrl.ApiKeyId)
	if !consumerExists || !issuerValid || presignedUrl.UserId != issuer.Id || len(presignedUrl.FileIds) == 0 {
		responseError(w, storage.ErrorInvalidPresign)
		return
	}
	files := make([]models.File, 0)
	for _, file := range presignedUrl.FileIds {
		storedFile, ok := storage.GetFile(file)
		if !ok {
			responseError(w, storage.ErrorFileNotFound)
			return
		}
		if !authorization.Authorize(issuer, authorization.ReadFile, storedFile.UserId) ||
			!authorization.Authorize(consumer, authorization.ReadFile, storedFile.UserId) {
			responseError(w, storage.ErrorInvalidPresign)
			return
		}
		files = append(files, storedFile)
	}
	if presignedUrl.IsArchive() {
		for _, file := range files {
			if file.Encryption.IsEndToEndEncrypted {
				responseError(w, storage.ErrorFileNotFound)
				return
			}
		}
	}
	for _, file := range files {
		if file.IsFileRequest() {
			serveCollectionPresign(w, r, presignedUrl, files, consumer)
			return
		}
	}
	if !presign.Consume(presignedUrl.Id) {
		responseError(w, storage.ErrorInvalidPresign)
		return
	}

	if !presignedUrl.IsArchive() {
		file := files[0]
		forceDecryption := file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted
		storage.ServeFile(file, w, r, true, false, forceDecryption, false)
		return
	}
	storage.ServeFilesAsZip(files, presignedUrl.Filename, w, r)
}

func serveFile(id string, isRootUrl bool, w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	savedFile, ok := storage.GetFile(id)

	if !ok || savedFile.IsFileRequest() {
		if isRootUrl {
			redirectOnIncorrectId(w, r, "error")
		} else {
			redirectOnIncorrectId(w, r, "../../error")
		}
		return
	}
	if savedFile.PasswordHash != "" {
		if !(isValidPwCookie(r, savedFile)) {
			if isRootUrl {
				redirect(w, r, "d?id="+savedFile.Id)
			} else {
				redirect(w, r, "../../d?id="+savedFile.Id)
			}
			return
		}
	}
	validFile := storage.ServeFile(savedFile, w, r, true, true, false, true)
	if !validFile {
		// Only called if the file has already expired during the expiry check of storage.ServeFile()
		if isRootUrl {
			redirectOnIncorrectId(w, r, "error")
		} else {
			redirectOnIncorrectId(w, r, "../../error")
		}
		return
	}
}

func requireLogin(next http.HandlerFunc, isUiCall, isPwChangeView bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addNoCacheHeader(w)
		user, isLoggedIn, err := authentication.IsAuthenticated(w, r)
		if err != nil {
			errorHandling.RedirectToErrorPage(w, r, "error.login.title", err.Error(), errorHandling.WidthDefault)
			return
		}
		if isLoggedIn {
			if user.ResetPassword && isUiCall && configuration.Get().Authentication.Method == models.AuthenticationInternal {
				if !isPwChangeView {
					redirect(w, r, "changePassword")
					return
				}
			}
			r = authentication.SetUserInRequest(r, user)
			next.ServeHTTP(w, r)
			return
		}
		if !isUiCall {
			w.WriteHeader(http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			_, _ = io.WriteString(w, "{\"Result\":\"error\",\"ErrorMessage\":\"Not authenticated\"}")
			return
		}
		redirect(w, r, "login")
	}
}

type adminButtonContext struct {
	CurrentFile models.FileApiOutput
	ActiveUser  *models.User
	UI          pageUI
}

// Used internally in templates to create buttons with user context
func newAdminButtonContext(file models.FileApiOutput, user models.User, ui ...pageUI) adminButtonContext {
	result := adminButtonContext{CurrentFile: file, ActiveUser: &user}
	if len(ui) > 0 {
		result.UI = ui[0]
	}
	return result
}

// Write a cookie if the user has entered a correct password for a password-protected file
func writeFilePwCookie(w http.ResponseWriter, file models.File) {
	http.SetCookie(w, &http.Cookie{
		Name:     "p" + file.Id,
		Value:    downloadPasswordToken.Generate(file.Id),
		Expires:  time.Now().Add(5 * time.Minute),
		HttpOnly: true,
		Secure:   configuration.UsesHTTPS(),
		SameSite: http.SameSiteStrictMode,
	})
}

// Checks if a cookie contains the correct token for a password-protected file
func isValidPwCookie(r *http.Request, file models.File) bool {
	cookie, err := r.Cookie("p" + file.Id)
	if err != nil {
		return false
	}
	return downloadPasswordToken.IsValid(cookie.Value, file.Id)
}

// Adds a header to disable external caching
func addNoCacheHeader(w http.ResponseWriter) {
	w.Header().Set("cdn-cache-control", "no-store, no-cache")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store, no-cache")
	w.Header().Set("cache-control", "no-store, no-cache")
	w.Header().Set("Pragma", "no-cache")
}

func addCacheHeader(w http.ResponseWriter) {
	w.Header().Set("cdn-cache-control", "public, max-age=36000")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "public, max-age=36000")
	w.Header().Set("cache-control", "public, max-age=36000")
}

// A view containing parameters for a generic template
type genericView struct {
	pageView
	IsAdminView       bool
	IsDownloadView    bool
	IsGenericError    bool
	PublicName        string
	RedirectUrl       string
	ErrorTitle        string
	ErrorMessage      string
	ErrorOauthMessage string
	CsrfToken         string
	ErrorCardWidth    string
	ErrorId           int
	MinPasswordLength int
	CustomContent     customStatic
}

// A view containing parameters for an oauth error
type oauthErrorView struct {
	pageView
	IsAdminView          bool
	IsDownloadView       bool
	PublicName           string
	IsAuthDenied         bool
	ErrorGenericMessage  string
	ErrorProvidedName    string
	ErrorProvidedMessage string
	CustomContent        customStatic
}

// A view containing parameters for the public upload page
type publicUploadView struct {
	pageView
	IsAdminView    bool
	IsDownloadView bool
	PublicName     string
	ChunkSize      int
	MaxServerSize  int
	CustomContent  customStatic
	FileRequest    *models.FileRequest
}
