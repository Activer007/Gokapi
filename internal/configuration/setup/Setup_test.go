package setup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/cloudconfig"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/environment"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

var jsonForms []jsonFormObject

func TestMain(m *testing.M) {
	testconfiguration.SetDirEnv()
	exitVal := m.Run()
	testconfiguration.Delete()
	os.Exit(exitVal)
}

func TestDebugNotSet(t *testing.T) {
	test.IsEqualBool(t, debugDisableAuth, false)
	if debugDisableAuth {
		fmt.Println("Debug mode is still on! Exiting test")
		os.Exit(1)
	}
}

func TestInputToJson(t *testing.T) {
	var err error
	_, r := test.GetRecorder("POST", "/setupResult", nil, nil, bytes.NewBufferString("invalid"))
	jsonForms, err = inputToJsonForm(r)
	test.IsNotNil(t, err)
	setupValues := createInputInternalAuth()
	buf := bytes.NewBufferString(setupValues.toJson())
	_, r = test.GetRecorder("POST", "/setupResult", nil, nil, buf)
	jsonForms, err = inputToJsonForm(r)
	test.IsNil(t, err)
	for _, item := range jsonForms {
		if item.Name == "auth_username" {
			test.IsEqualString(t, item.Value, "admin")
		}
	}
}

func TestMissingSetupValues(t *testing.T) {
	invalidInputs := createInvalidSetupValues()
	for _, invalid := range invalidInputs {
		formObjects, err := invalid.toFormObject()
		test.IsNil(t, err)
		_, _, _, _, err = toConfiguration(&formObjects)
		test.IsNotNilWithMessage(t, err, invalid.toJson())
	}
}

func TestEncryptionSetup(t *testing.T) {
	var e2eConfig configuration.End2EndReconfigParameters
	input := createInputOAuth()
	input.EncryptionLevel.Value = "1"
	formObjects, err := input.toFormObject()
	test.IsNil(t, err)
	config, _, e2eConfig, _, err = toConfiguration(&formObjects)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(config.Encryption.Cipher), 32)
	test.IsEqualString(t, config.Encryption.Checksum, "")
	test.IsEqualBool(t, e2eConfig.DeleteEncryptedStorage, false)
	test.IsEqualBool(t, e2eConfig.DeleteEnd2EndEncryption, false)

	input.EncryptionLevel.Value = "2"
	input.EncryptionPassword.Value = "testpw12"
	formObjects, err = input.toFormObject()
	test.IsNil(t, err)
	config, _, _, _, err = toConfiguration(&formObjects)
	test.IsNil(t, err)
	test.IsEqualString(t, string(config.Encryption.Cipher), "")
	test.IsEqualInt(t, len(config.Encryption.Checksum), 64)

	isInitialSetup = false

	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	configuration.Get().Encryption.Level = 3
	id := testconfiguration.WriteEncryptedFile()
	file, ok := database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, file.UnlimitedTime, true)
	formObjects, err = input.toFormObject()
	test.IsNil(t, err)
	config, _, e2eConfig, _, err = toConfiguration(&formObjects)
	test.IsNil(t, err)
	test.IsEqualBool(t, e2eConfig.DeleteEncryptedStorage, true)
	test.IsEqualBool(t, e2eConfig.DeleteEnd2EndEncryption, false)

	configuration.Get().Encryption.Level = 2
	input.EncryptionPassword.Value = "unc"
	id = testconfiguration.WriteEncryptedFile()
	_, ok = database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	formObjects, err = input.toFormObject()
	test.IsNil(t, err)
	config, _, _, _, err = toConfiguration(&formObjects)
	test.IsNil(t, err)
	file, ok = database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, file.UnlimitedTime, true)

	_, ok = database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	configuration.Get().Encryption.Level = 2
	input.EncryptionPassword.Value = "otherpw12"
	id = testconfiguration.WriteEncryptedFile()
	_, ok = database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	formObjects, err = input.toFormObject()
	test.IsNil(t, err)
	config, _, e2eConfig, _, err = toConfiguration(&formObjects)
	test.IsNil(t, err)
	test.IsEqualBool(t, e2eConfig.DeleteEncryptedStorage, true)
	test.IsEqualBool(t, e2eConfig.DeleteEnd2EndEncryption, false)

	database.Close()
	testconfiguration.Delete()

	isInitialSetup = true
}

var config = models.Configuration{
	Authentication: models.AuthenticationConfig{},
	Port:           "",
	ServerUrl:      "",
	RedirectUrl:    "",
	ConfigVersion:  0,
	DataDir:        "",
	MaxMemory:      0,
	UseSsl:         false,
	MaxFileSizeMB:  0,
}

func TestToConfiguration(t *testing.T) {
	output, cloudConfig, _, authsettings, err := toConfiguration(&jsonForms)
	test.IsNil(t, err)
	test.IsEqualInt(t, output.Authentication.Method, models.AuthenticationInternal)
	test.IsEqualString(t, cloudConfig.Aws.KeyId, "testapi")
	test.IsEqualString(t, output.Authentication.Username, "admin")
	test.IsNotEqualString(t, authsettings.PasswordInternalAuth, "adminadmin")
	test.IsNotEqualString(t, authsettings.PasswordInternalAuth, "")
	test.IsEqualString(t, output.RedirectUrl, "https://github.com/Forceu/Gokapi/")
}

func TestVerifyPortNumber(t *testing.T) {
	test.IsEqualInt(t, verifyPortNumber(2134), 2134)
	test.IsEqualInt(t, verifyPortNumber(-1), environment.DefaultPort)
	test.IsEqualInt(t, verifyPortNumber(666666), environment.DefaultPort)
}

func TestAddTrailingSlash(t *testing.T) {
	test.IsEqualString(t, addTrailingSlash("test"), "test/")
	test.IsEqualString(t, addTrailingSlash("test2/"), "test2/")
}

func TestIsPathMounted(t *testing.T) {
	mounts := strings.NewReader("overlay / overlay rw 0 0\n/dev/vda /mnt_vol ext4 rw 0 0\n")
	test.IsEqualBool(t, isPathMounted(mounts, "/mnt_vol/data"), true)

	mounts = strings.NewReader("overlay / overlay rw 0 0\n/dev/vdb /app/data ext4 rw 0 0\n")
	test.IsEqualBool(t, isPathMounted(mounts, "data"), true)

	mounts = strings.NewReader("overlay / overlay rw 0 0\n")
	test.IsEqualBool(t, isPathMounted(mounts, "/mnt_vol/data"), false)
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	outputError(w, errors.New("test error"))
	test.IsEqualInt(t, w.Result().StatusCode, 500)
	test.ResponseBodyContains(t, w, "test error")
}

func TestBasicAuth(t *testing.T) {
	setupComplete.Store(false)
	isAuth := false
	continueFunc := func(w http.ResponseWriter, r *http.Request) {
		isAuth = true
	}

	w, r := test.GetRecorder("GET", "/setup", nil, nil, nil)
	isInitialSetup = true
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, false)

	isAuth = false
	isInitialSetup = false
	credentialUsername = "test"
	credentialPassword = "testpw"
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, false)

	r.Header.Add("Authorization", "Basic dGVzdDp0ZXN0cHc=")
	isAuth = false
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, true)

	r.Header.Set("Authorization", "Basic dGVzdDppbnZhbGlk")
	isAuth = false
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, false)

	r.Header.Set("Authorization", "Basic aW52YWxpZDppbnZhbGlk")
	isAuth = false
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, false)

	r.Header.Set("Authorization", "Basic aW52YWxpZDp0ZXN0cHc=")
	isAuth = false
	basicAuth(continueFunc).ServeHTTP(w, r)
	test.IsEqualBool(t, isAuth, false)
}

func TestInitialSetup(t *testing.T) {
	testconfiguration.Create(false)
	test.CompletesWithinTime(t, RunIfFirstStart, 3*time.Second)
	testconfiguration.Delete()
	server := startSetupTestServer(t, runIfFirstStart)
	server.stop(t)
	test.IsEqualBool(t, isInitialSetup, true)
}

type dbFormTest struct {
	DatabaseType   string `form:"dbtype_sel"`
	SqliteLocation string `form:"sqlite_location"`
	RedisLocation  string `form:"redis_location"`
	RedisPrefix    string `form:"redis_prefix"`
	RedisUser      string `form:"redis_user"`
	RedisPw        string `form:"redis_password"`
	RedisUseSsl    string `form:"redis_ssl_sel"`
}

func generateDbFormValues(input dbFormTest) []jsonFormObject {
	result := make([]jsonFormObject, 0)
	v := reflect.ValueOf(input)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		result = append(result, jsonFormObject{
			Name:  t.Field(i).Tag.Get("form"),
			Value: v.Field(i).Interface().(string),
		})
	}
	return result
}

func TestParseDatabaseSettings(t *testing.T) {
	output := models.Configuration{}
	input := generateDbFormValues(dbFormTest{
		DatabaseType:   "0",
		SqliteLocation: "./data/test.sqlite",
		RedisUseSsl:    "0",
	})
	expected := "sqlite://./data/test.sqlite"
	err := parseDatabaseSettings(&output, &input)
	test.IsNil(t, err)
	test.IsEqualString(t, output.DatabaseUrl, expected)

	input = generateDbFormValues(dbFormTest{
		DatabaseType:  "1",
		RedisLocation: "127.0.0.1:1234",
		RedisUseSsl:   "0",
	})
	expected = "redis://127.0.0.1:1234"
	err = parseDatabaseSettings(&output, &input)
	test.IsNil(t, err)
	test.IsEqualString(t, output.DatabaseUrl, expected)

	input = generateDbFormValues(dbFormTest{
		DatabaseType:  "1",
		RedisLocation: "127.0.0.1:1234",
		RedisPrefix:   "pre_",
		RedisUser:     "testuser",
		RedisPw:       "testpw",
		RedisUseSsl:   "1",
	})
	expected = "redis://testuser:testpw@127.0.0.1:1234?prefix=pre_&ssl=true"
	err = parseDatabaseSettings(&output, &input)
	test.IsNil(t, err)
	test.IsEqualString(t, output.DatabaseUrl, expected)
}

func TestRunConfigModification(t *testing.T) {
	testconfiguration.Create(false)
	credentialUsername = ""
	credentialPassword = ""
	server := startSetupTestServer(t, runConfigModification)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/start",
		IsHtml:          false,
		ExcludedContent: []string{"Gokapi Setup"},
		Method:          "GET",
		ResultCode:      401,
	})
	server.stop(t)
	test.IsEqualString(t, credentialUsername, "setup")
	test.IsEqualInt(t, len(credentialPassword), 32)
	isInitialSetup = true
}

type setupTestServer struct {
	URL       string
	addr      string
	lifecycle *setupTestLifecycle
}

type setupTestReady struct {
	listener net.Listener
}

// Cancellation owns the listener lifecycle, including a factory that returns
// after readiness has timed out. No mutable global listener hook is restored.
type setupTestLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	ready  chan setupTestReady
	done   chan struct{}
	mu     sync.Mutex
	server *http.Server
}

func newSetupTestLifecycle() *setupTestLifecycle {
	ctx, cancel := context.WithCancel(context.Background())
	return &setupTestLifecycle{ctx: ctx, cancel: cancel, ready: make(chan setupTestReady), done: make(chan struct{})}
}

func (lifecycle *setupTestLifecycle) listenerFactory(open func() (net.Listener, error)) setupListenerFactory {
	return func(server *http.Server) (net.Listener, error) {
		lifecycle.mu.Lock()
		lifecycle.server = server
		lifecycle.mu.Unlock()
		listener, err := open()
		if err != nil {
			return nil, err
		}
		// Check cancellation before publishing a late listener. Returning a
		// closed listener to a closed server lets Serve exit normally.
		if lifecycle.ctx.Err() != nil {
			_ = listener.Close()
			_ = server.Close()
			return listener, nil
		}
		select {
		case lifecycle.ready <- setupTestReady{listener: listener}:
		case <-lifecycle.ctx.Done():
			_ = listener.Close()
			_ = server.Close()
		}
		return listener, nil
	}
}

func (lifecycle *setupTestLifecycle) waitStopped(t *testing.T) {
	t.Helper()
	select {
	case <-lifecycle.done:
	case <-time.After(10 * time.Second):
		t.Fatal("setup completion timeout")
	}
}

func (lifecycle *setupTestLifecycle) stop(t *testing.T) {
	t.Helper()
	lifecycle.cancel()
	lifecycle.mu.Lock()
	server := lifecycle.server
	lifecycle.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			t.Errorf("setup shutdown failed: %v", err)
		}
	}
	lifecycle.waitStopped(t)
}

// The same allocated listener is handed to Serve. Cleanup is registered before
// launch, and readiness/completion are synchronized separately for each cycle.
func startSetupTestServer(t *testing.T, run func(setupListenerFactory)) *setupTestServer {
	t.Helper()
	lifecycle := newSetupTestLifecycle()
	t.Cleanup(func() { lifecycle.stop(t) })
	listen := lifecycle.listenerFactory(func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") })
	go func() { run(listen); close(lifecycle.done) }()
	var current setupTestReady
	select {
	case current = <-lifecycle.ready:
	case <-lifecycle.done:
		t.Fatal("setup returned without allocating a listener")
	case <-time.After(10 * time.Second):
		lifecycle.cancel()
		t.Fatal("setup listener readiness timeout")
	}
	server := &setupTestServer{URL: "http://" + current.listener.Addr().String(), addr: current.listener.Addr().String(), lifecycle: lifecycle}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !isServerRunning(t, server.URL) {
		select {
		case <-lifecycle.done:
			t.Fatal("setup stopped before HTTP readiness")
		case <-deadline.C:
			lifecycle.cancel()
			t.Fatal("setup HTTP readiness timeout")
		case <-tick.C:
		}
	}
	return server
}

func (server *setupTestServer) waitStopped(t *testing.T) { server.lifecycle.waitStopped(t) }
func (server *setupTestServer) stop(t *testing.T)        { server.lifecycle.stop(t) }

func TestSEC062LateListenerCleanup(t *testing.T) {
	lifecycle := newSetupTestLifecycle()
	// Register both cleanup actions before launch. Even an early test failure
	// releases the synthetic gate before waiting for the serving goroutine.
	t.Cleanup(func() { lifecycle.stop(t) })
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	openGate := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(openGate)
	allocated := make(chan net.Listener, 1)
	served := make(chan error, 1)
	listen := lifecycle.listenerFactory(func() (net.Listener, error) {
		close(entered)
		<-release
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			allocated <- listener
		}
		return listener, err
	})
	go func() {
		server := &http.Server{Handler: http.NotFoundHandler()}
		listener, err := listen(server)
		if err == nil {
			err = server.Serve(listener)
		}
		served <- err
		close(lifecycle.done)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("delayed factory did not start")
	}
	// Model the readiness timeout/cancellation before the listener exists.
	lifecycle.cancel()
	openGate()
	var listener net.Listener
	select {
	case listener = <-allocated:
	case <-time.After(3 * time.Second):
		t.Fatal("delayed listener was not allocated")
	}
	defer listener.Close()
	select {
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("cancelled Serve = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late listener left a serving goroutine")
	}
	// A deadline makes a leaked listener fail promptly rather than hanging
	// the regression. A properly closed listener returns net.ErrClosed.
	_ = listener.(*net.TCPListener).SetDeadline(time.Now())
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("late listener was not closed: %v", err)
	}
	lifecycle.waitStopped(t)
}

func isServerRunning(t *testing.T, baseURL string) bool {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	req, err := http.NewRequest("GET", baseURL+"/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	return err == nil
}

func TestIntegration(t *testing.T) {
	testconfiguration.Delete()
	test.FileDoesNotExist(t, "test/config.json")
	server := startSetupTestServer(t, runIfFirstStart)
	t.Run("SEC014SlowSetupHeader", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", server.addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(7 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(conn, "GET /setup/start HTTP/1.1\r\nHost: localhost\r\nX-Slow: "); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			t.Fatal("setup header outlived the short header deadline")
		}
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Fatal("incomplete setup request reached handler")
			}
		}
	})
	proof, err := os.ReadFile(credentialFile)
	test.IsNil(t, err)
	initialAuth := []test.Header{{Name: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte("setup:"+strings.TrimSpace(string(proof))))}}
	unauthorizedInput := createInputInternalAuth()
	// A real installation request without the local proof must not write config.
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url: server.URL + "/setup/setupResult", Method: "POST", ResultCode: 401,
		Body: strings.NewReader(unauthorizedInput.toJson()),
	})
	test.FileDoesNotExist(t, "test/config.json")

	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             server.URL + "/admin",
		IsHtml:          false,
		RequiredContent: []string{"Server is in maintenance mode"},
		ExcludedContent: []string{"Downloads"},
		Method:          "GET",
		ResultCode:      200,
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/start",
		IsHtml:          false,
		RequiredContent: []string{"Thank you for choosing Gokapi"},
		Headers:         initialAuth,
		Method:          "GET",
		ResultCode:      200,
	})
	test.HttpPostRequest(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/setupResult",
		ExcludedContent: []string{"\"result\": \"OK\""},
		RequiredContent: []string{"\"result\": \"Error\""},
		IsHtml:          false,
		Method:          "POST",
		PostValues:      nil,
		Headers:         initialAuth,
		ResultCode:      500,
	})

	setupVals := createInputInternalAuth()
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/setupResult",
		RequiredContent: []string{"\"result\": \"OK\""},
		ExcludedContent: []string{"\"result\": \"Error\""},
		IsHtml:          false,
		Method:          "POST",
		ResultCode:      200,
		Body:            strings.NewReader(setupVals.toJson()),
		Headers:         initialAuth,
	})
	test.FileDoesNotExist(t, credentialFile)
	replayInput := createInputHeaderAuth()
	// The successful installation consumes the proof before the server stops.
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url: server.URL + "/setup/setupResult", Method: "POST", ResultCode: 401,
		Headers: initialAuth, Body: strings.NewReader(replayInput.toJson()),
	})

	server.waitStopped(t)
	test.FileExists(t, "test/config.json")
	settings := configuration.Get()
	test.IsEqualBool(t, settings.IncludeFilename, true)
	test.IsEqualInt(t, settings.Authentication.Method, 0)
	test.IsEqualString(t, settings.Authentication.Username, "admin")
	test.IsEqualString(t, settings.Authentication.OAuthProvider, "")
	test.IsEqualString(t, settings.Authentication.OAuthClientId, "")
	test.IsEqualString(t, settings.Authentication.OAuthClientSecret, "")
	test.IsEqualBool(t, settings.Authentication.OnlyRegisteredUsers, false)
	test.IsEqualString(t, settings.Authentication.HeaderKey, "")
	test.IsEqualBool(t, strings.Contains(settings.Port, "127.0.0.1"), true)
	test.IsEqualBool(t, strings.Contains(settings.Port, ":53842"), true)
	test.IsEqualBool(t, settings.UseSsl, false)
	test.IsEqualBool(t, settings.SaveIp, false)
	test.IsEqualString(t, settings.ServerUrl, "http://127.0.0.1:53842/")
	test.IsEqualString(t, settings.RedirectUrl, "https://github.com/Forceu/Gokapi/")
	cconfig, ok := cloudconfig.Load()
	test.IsEqualBool(t, ok, true)
	if os.Getenv("GOKAPI_AWS_BUCKET") == "" {
		test.IsEqualString(t, cconfig.Aws.Bucket, "testbucket")
		test.IsEqualString(t, cconfig.Aws.Region, "testregion")
		test.IsEqualString(t, cconfig.Aws.KeyId, "testapi")
		test.IsEqualString(t, cconfig.Aws.KeySecret, "testsecret")
		test.IsEqualString(t, cconfig.Aws.Endpoint, "testendpoint")
		test.IsEqualBool(t, cconfig.Aws.ProxyDownload, true)
	}
	test.FileExists(t, "test/cloudconfig.yml")

	server = startSetupTestServer(t, runConfigModification)

	credentialUsername = "test"
	credentialPassword = "testpw"

	setupInput := createInputHeaderAuth()
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/start",
		RequiredContent: []string{"Unauthorized"},
		ExcludedContent: []string{"\"result\":"},
		IsHtml:          false,
		Method:          "POST",
		ResultCode:      401,
		Body:            strings.NewReader(setupInput.toJson()),
	})
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/start",
		RequiredContent: []string{"You can now change the Gokapi configuration."},
		ExcludedContent: []string{"Unauthorized"},
		IsHtml:          false,
		Method:          "POST",
		Headers:         []test.Header{{Name: "Authorization", Value: "Basic dGVzdDp0ZXN0cHc="}},
		ResultCode:      200,
	})

	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/setupResult",
		RequiredContent: []string{"Unauthorized"},
		ExcludedContent: []string{"\"result\":"},
		IsHtml:          false,
		Method:          "POST",
		ResultCode:      401,
		Body:            strings.NewReader(setupInput.toJson()),
	})
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/setupResult",
		RequiredContent: []string{"\"result\": \"OK\""},
		ExcludedContent: []string{"\"result\": \"Error\""},
		IsHtml:          false,
		Method:          "POST",
		Headers:         []test.Header{{Name: "Authorization", Value: "Basic dGVzdDp0ZXN0cHc="}},
		ResultCode:      200,
		Body:            strings.NewReader(setupInput.toJson()),
	})

	server.waitStopped(t)

	test.FileExists(t, "test/config.json")
	settings = configuration.Get()
	test.IsEqualBool(t, settings.IncludeFilename, false)
	test.IsEqualInt(t, settings.Authentication.Method, 2)
	test.IsEqualString(t, settings.Authentication.Username, "test1")
	test.IsEqualString(t, settings.Authentication.OAuthProvider, "")
	test.IsEqualString(t, settings.Authentication.OAuthClientId, "")
	test.IsEqualString(t, settings.Authentication.OAuthClientSecret, "")
	test.IsEqualString(t, settings.Authentication.HeaderKey, "testkey")
	test.IsEqualBool(t, settings.Authentication.OnlyRegisteredUsers, true)
	test.IsEqualBool(t, strings.Contains(settings.Port, "127.0.0.1"), false)
	test.IsEqualBool(t, strings.Contains(settings.Port, ":53842"), true)
	test.IsEqualBool(t, settings.UseSsl, true)
	test.IsEqualBool(t, settings.SaveIp, true)
	test.IsEqualString(t, settings.ServerUrl, "http://127.0.0.1:53842/")
	test.IsEqualString(t, settings.RedirectUrl, "https://test.com")
	test.IsEqualBool(t, settings.PicturesAlwaysLocal, false)
	cconfig, ok = cloudconfig.Load()
	if os.Getenv("GOKAPI_AWS_BUCKET") == "" {
		test.IsEqualBool(t, ok, false)
	}
	test.FileDoesNotExist(t, "test/cloudconfig.yml")

	server = startSetupTestServer(t, runConfigModification)

	credentialUsername = "test"
	credentialPassword = "testpw"

	setupInput = createInputOAuth()
	test.HttpPageResultJson(t, test.HttpTestConfig{
		Url:             server.URL + "/setup/setupResult",
		RequiredContent: []string{"\"result\": \"OK\""},
		ExcludedContent: []string{"\"result\": \"Error\""},
		IsHtml:          false,
		Method:          "POST",
		Headers:         []test.Header{{Name: "Authorization", Value: "Basic dGVzdDp0ZXN0cHc="}},
		ResultCode:      200,
		Body:            strings.NewReader(setupInput.toJson()),
	})

	server.waitStopped(t)

	test.IsEqualBool(t, settings.PicturesAlwaysLocal, true)
	test.IsEqualBool(t, cconfig.Aws.ProxyDownload, false)
	test.IsEqualString(t, settings.Authentication.OAuthProvider, "provider")
	test.IsEqualString(t, settings.Authentication.OAuthClientId, "id")
	test.IsEqualString(t, settings.Authentication.OAuthClientSecret, "secret")
	test.IsEqualString(t, settings.Authentication.Username, "oatest1")
	test.IsEqualBool(t, settings.Authentication.OnlyRegisteredUsers, true)
}

type setupValues struct {
	BindLocalhost                 setupEntry `form:"localhost_sel" isBool:"true"`
	UseSsl                        setupEntry `form:"ssl_sel" isBool:"true"`
	SaveIp                        setupEntry `form:"logip_sel" isBool:"true"`
	Port                          setupEntry `form:"port" isInt:"true"`
	PublicName                    setupEntry `form:"public_name"`
	ExtUrl                        setupEntry `form:"url"`
	RedirectUrl                   setupEntry `form:"url_redirection"`
	IncludeFilename               setupEntry `form:"showfilename_sel" isBool:"true"`
	AuthenticationMode            setupEntry `form:"authentication_sel" isInt:"true"`
	AuthUsername                  setupEntry `form:"auth_username"`
	AuthPassword                  setupEntry `form:"auth_pw"`
	OAuthProvider                 setupEntry `form:"oauth_provider"`
	OAuthClientId                 setupEntry `form:"oauth_id"`
	OAuthClientSecret             setupEntry `form:"oauth_secret"`
	OAuthAuthorisedGroups         setupEntry `form:"oauth_allowed_groups"`
	OAuthAdminUser                setupEntry `form:"oauth_admin_user"`
	OAuthScopeGroup               setupEntry `form:"oauth_scope_groups"`
	OAuthRestrictGroups           setupEntry `form:"oauth_restrict_groups" isBool:"true"`
	OAuthRecheckInterval          setupEntry `form:"oauth_recheck_interval" isInt:"true"`
	OAuthOnlyRegisteredUsers      setupEntry `form:"oauth_only_registered_users" isBool:"true"`
	AuthHeaderKey                 setupEntry `form:"auth_headerkey"`
	AuthHeaderAdmin               setupEntry `form:"auth_header_admin"`
	AuthHeaderOnlyRegisteredUsers setupEntry `form:"auth_header_only_registered_users" isBool:"true"`
	StorageSelection              setupEntry `form:"storage_sel"`
	PicturesAlwaysLocal           setupEntry `form:"storage_sel_image"`
	ProxyDownloads                setupEntry `form:"storage_sel_proxy"`
	S3Bucket                      setupEntry `form:"s3_bucket"`
	S3Region                      setupEntry `form:"s3_region"`
	S3ApiKey                      setupEntry `form:"s3_api"`
	S3ApiSecret                   setupEntry `form:"s3_secret"`
	S3Endpoint                    setupEntry `form:"s3_endpoint"`
	EncryptionLevel               setupEntry `form:"encrypt_sel" isInt:"true"`
	EncryptionPassword            setupEntry `form:"enc_pw"`
	DatabaseType                  setupEntry `form:"dbtype_sel" isInt:"true"`
	SqliteLocation                setupEntry `form:"sqlite_location"`
	RedisLocation                 setupEntry `form:"redis_location"`
	RedisPrefix                   setupEntry `form:"redis_prefix"`
	RedisUser                     setupEntry `form:"redis_user"`
	RedisPw                       setupEntry `form:"redis_password"`
	RedisUseSsl                   setupEntry `form:"redis_ssl_sel" isBool:"true"`
}

func (s *setupValues) init() {
	v := reflect.ValueOf(s)
	t := reflect.TypeOf(setupValues{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("form")
		v.Elem().Field(i).FieldByName("FormName").SetString(tag)
		v.Elem().Field(i).FieldByName("Enabled").SetBool(true)
	}
}

func (s *setupValues) toJson() string {
	var values []jsonFormObject

	v := reflect.ValueOf(s)
	t := reflect.TypeOf(setupValues{})
	for i := 0; i < t.NumField(); i++ {
		values = append(values, jsonFormObject{
			Name:  v.Elem().Field(i).FieldByName("FormName").String(),
			Value: v.Elem().Field(i).FieldByName("Value").String(),
		})
	}

	result, err := json.Marshal(values)
	if err != nil {
		log.Fatal(err)
	}
	return string(result)
}

func (s *setupValues) toFormObject() ([]jsonFormObject, error) {
	r := httptest.NewRequest("POST", "/setup", strings.NewReader(s.toJson()))
	setupResult, err := inputToJsonForm(r)
	if err != nil {
		return nil, err
	}
	return setupResult, err
}

func createInvalidSetupValues() []setupValues {
	var result []setupValues
	input := createInputInternalAuth()
	t := reflect.TypeOf(setupValues{})
	for i := 0; i < t.NumField(); i++ {
		invalidSetup := input
		v := reflect.ValueOf(&invalidSetup)
		v.Elem().Field(i).FieldByName("FormName").SetString("XXXinvalidNameXXX")
		result = append(result, invalidSetup)

		tag := t.Field(i).Tag.Get("isInt")
		if tag == "true" {
			invalidSetup = input
			v.Elem().Field(i).FieldByName("Value").SetString("XXXnotIntXXX")
			result = append(result, invalidSetup)
		}
		tag = t.Field(i).Tag.Get("isBool")
		if tag == "true" {
			invalidSetup = input
			v.Elem().Field(i).FieldByName("Value").SetString("2")
			result = append(result, invalidSetup)
		}
	}
	invalidSetup := input
	invalidSetup.AuthenticationMode.Value = "4"
	result = append(result, invalidSetup)

	invalidSetup = input
	invalidSetup.EncryptionLevel.Value = "-1"
	result = append(result, invalidSetup)

	invalidSetup = input
	invalidSetup.EncryptionLevel.Value = "9"
	result = append(result, invalidSetup)

	invalidSetup = input
	invalidSetup.EncryptionLevel.Value = "4"
	invalidSetup.EncryptionPassword.Value = "2shrt"
	result = append(result, invalidSetup)

	return result
}

type setupEntry struct {
	FormName string
	Enabled  bool
	Value    string
}

func createInputInternalAuth() setupValues {
	values := setupValues{}
	values.init()

	values.BindLocalhost.Value = "1"
	values.PublicName.Value = "Test Name"
	values.UseSsl.Value = "0"
	values.IncludeFilename.Value = "1"
	values.Port.Value = "53842"
	values.ExtUrl.Value = "http://127.0.0.1:53842/"
	values.RedirectUrl.Value = "https://github.com/Forceu/Gokapi/"
	values.AuthenticationMode.Value = "0"
	values.AuthUsername.Value = "admin"
	values.AuthPassword.Value = "adminadmin"
	values.StorageSelection.Value = "cloud"
	values.ProxyDownloads.Value = "proxy"
	values.S3Bucket.Value = "testbucket"
	values.S3Region.Value = "testregion"
	values.S3ApiKey.Value = "testapi"
	values.S3ApiSecret.Value = "testsecret"
	values.S3Endpoint.Value = "testendpoint"
	values.EncryptionLevel.Value = "0"
	values.PicturesAlwaysLocal.Value = "nochange"
	values.SaveIp.Value = "0"
	values.OAuthOnlyRegisteredUsers.Value = "false"
	values.OAuthRestrictGroups.Value = "false"
	values.OAuthRecheckInterval.Value = "12"
	values.AuthHeaderOnlyRegisteredUsers.Value = "false"
	values.DatabaseType.Value = "0"
	values.SqliteLocation.Value = "./test/gokapi.sqlite"
	values.RedisUseSsl.Value = "0"

	return values
}

func createInputHeaderAuth() setupValues {
	values := setupValues{}
	values.init()

	values.BindLocalhost.Value = "0"
	values.PublicName.Value = "Test Name"
	values.UseSsl.Value = "1"
	values.Port.Value = "53842"
	values.ExtUrl.Value = "http://127.0.0.1:53842/"
	values.RedirectUrl.Value = "https://test.com"
	values.AuthenticationMode.Value = "2"
	values.AuthHeaderKey.Value = "testkey"
	values.AuthHeaderAdmin.Value = "test1"
	values.AuthHeaderOnlyRegisteredUsers.Value = "true"
	values.StorageSelection.Value = "local"
	values.EncryptionLevel.Value = "0"
	values.SaveIp.Value = "1"
	values.OAuthOnlyRegisteredUsers.Value = "false"
	values.OAuthRestrictGroups.Value = "false"
	values.OAuthRecheckInterval.Value = "12"
	values.IncludeFilename.Value = "0"
	values.DatabaseType.Value = "0"
	values.SqliteLocation.Value = "./test/gokapi.sqlite"
	values.RedisUseSsl.Value = "0"

	return values
}

func createInputOAuth() setupValues {
	values := createInputHeaderAuth()
	values.AuthenticationMode.Value = "1"
	values.OAuthProvider.Value = "provider"
	values.OAuthClientId.Value = "id"
	values.OAuthClientSecret.Value = "secret"
	values.OAuthOnlyRegisteredUsers.Value = "true"
	values.OAuthRestrictGroups.Value = "true"
	values.OAuthAdminUser.Value = "oatest1"
	values.OAuthScopeGroup.Value = "groups"
	values.OAuthAuthorisedGroups.Value = "group1; group2"
	values.StorageSelection.Value = "local"
	values.PicturesAlwaysLocal.Value = "local"
	values.ProxyDownloads.Value = "default"
	return values
}

func TestIsErrorAddressAlreadyInUse(t *testing.T) {

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	srv2 := http.Server{
		Addr: l.Addr().String(),
	}
	httpError := make(chan error)
	go func() {
		sErr := srv2.ListenAndServe()
		httpError <- sErr
	}()
	select {
	case err = <-httpError:
		test.IsEqualBool(t, isErrorAddressAlreadyInUse(err), true)
	case <-time.After(15 * time.Second):
		t.Fatalf("httpError timeout after 15 seconds")
	}
	err = errors.New("other error")
	test.IsEqualBool(t, isErrorAddressAlreadyInUse(err), false)
}
