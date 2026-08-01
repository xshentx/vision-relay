package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagementBootstrapSetsPrivateCookieAndCleansURL(t *testing.T) {
	called := 0
	handler := withManagementAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}), testManagementToken)

	bootstrap := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18473/?keep=1&management_token="+testManagementToken, nil)
	bootstrap.RemoteAddr = "127.0.0.1:5000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, bootstrap)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "http://127.0.0.1:18473/?keep=1" {
		t.Fatalf("bootstrap response = %d, location %q", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != managementTokenCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected management cookie: %#v", cookies)
	}
	if called != 0 {
		t.Fatal("bootstrap request reached the management page before URL cleanup")
	}

	authorized := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18473/api/config", nil)
	authorized.RemoteAddr = "127.0.0.1:5000"
	authorized.AddCookie(cookies[0])
	authorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("cookie-authenticated response = %d, calls = %d", authorizedResponse.Code, called)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18473/api/config", nil)
	unauthorized.RemoteAddr = "127.0.0.1:5000"
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated management response = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestManagementTokenFileIsStableAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", managementTokenFileName)
	first, err := loadOrCreateManagementToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateManagementToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !validAccessToken(first) {
		t.Fatalf("management token was not stable or valid")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("management token mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestNonLoopbackRelayRequiresAuthentication(t *testing.T) {
	cfg := defaultConfig()
	cfg.Addr = "0.0.0.0:8787"
	cfg.RelayToken = "relay-secret"
	calls := 0
	handler := withRelayAccess(&app{cfg: cfg, relayAuthRequired: true}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func(path, authorization string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if got := request("/v1/models", ""); got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated relay status = %d", got)
	}
	if got := request("/v1/models", "Bearer wrong"); got != http.StatusUnauthorized {
		t.Fatalf("wrong relay token status = %d", got)
	}
	if got := request("/v1/models", "Bearer relay-secret"); got != http.StatusNoContent {
		t.Fatalf("authenticated relay status = %d", got)
	}
	if got := request("/healthz", ""); got != http.StatusNoContent {
		t.Fatalf("relay health status = %d", got)
	}
	if calls != 2 {
		t.Fatalf("downstream handler calls = %d, want 2", calls)
	}
}

func TestSavedRelayAddressCannotWeakenActiveListenerAuthentication(t *testing.T) {
	cfg := defaultConfig()
	cfg.Addr = "127.0.0.1:8787" // Saved for the next restart; the active listener is still public.
	cfg.RelayToken = "relay-secret"
	handler := withRelayAccess(&app{cfg: cfg, relayAuthRequired: true}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("saved loopback address weakened active public listener: status = %d", unauthorized.Code)
	}
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer relay-secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authenticated active listener status = %d", authorized.Code)
	}
}

func TestLoopbackRelayRemainsAuthenticationCompatible(t *testing.T) {
	cfg := defaultConfig()
	cfg.Addr = "127.0.0.1:8787"
	handler := withRelayAccess(&app{cfg: cfg}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("loopback relay status = %d", recorder.Code)
	}
}

func TestRelayRequestBodyLimit(t *testing.T) {
	tooLargeByLength := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))
	tooLargeByLength.ContentLength = maxRelayRequestBodyBytes + 1
	if _, err := captureRequestBody(tooLargeByLength); err != errRequestBodyTooLarge {
		t.Fatalf("Content-Length limit error = %v", err)
	}

	unknownLength := httptest.NewRequest(http.MethodPost, "/v1/responses", io.LimitReader(zeroReader{}, maxRelayRequestBodyBytes+1))
	unknownLength.ContentLength = -1
	if _, err := captureRequestBody(unknownLength); err != errRequestBodyTooLarge {
		t.Fatalf("streamed body limit error = %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for i := range buffer {
		buffer[i] = 0
	}
	return len(buffer), nil
}

func TestCapturedRequestBodyIsReused(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"test"}`))
	captured, err := captureRequestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := readBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) == 0 || &captured[0] != &body[0] {
		t.Fatal("handler read allocated a second request-body copy")
	}
}

func TestUpstreamRedirectIsReturnedWithoutForwardingCredentials(t *testing.T) {
	var redirectedCalls int
	redirected := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		redirectedCalls++
		if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("upstream credential reached redirect target")
		}
	}))
	defer redirected.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	a := &app{httpClient: origin.Client()}
	response, err := a.forwardRawOnce(t.Context(), endpoint{Provider: "anthropic", BaseURL: origin.URL, APIKey: "upstream-secret"}, http.MethodPost, "/v1/messages", []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || redirectedCalls != 0 {
		t.Fatalf("redirect response = %d, target calls = %d", response.StatusCode, redirectedCalls)
	}
}

func TestClientConfigRequestRejectsMalformedAndAmbiguousJSON(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"client":"codex","unknown":true}`, `{"client":"codex"} {}`, `{"client":`} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/client/configure", strings.NewReader(body))
			if _, err := decodeClientConfigRequest(request); err == nil {
				t.Fatalf("decode accepted %q", body)
			}
		})
	}

	a := &app{cfg: defaultConfig()}
	recorder := httptest.NewRecorder()
	a.handleClientConfigure(recorder, httptest.NewRequest(http.MethodPost, "/api/client/configure", strings.NewReader(`{"client":"unsupported"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unsupported client status = %d", recorder.Code)
	}
}

func TestDefaultDatabasePathUsesExecutableDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	want := filepath.Join(filepath.Dir(executable), appSlug+".db")
	if !sameFilePath(defaultDBPath(), want) {
		t.Fatalf("default DB path = %q, want %q", defaultDBPath(), want)
	}
}

func TestLegacyMigrationReadsUserConfigurationDatabase(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("APPDATA", filepath.Join(tempDir, "config"))

	sourcePath := userConfigDBPath()
	if sourcePath == "" {
		t.Fatal("user configuration database path is empty")
	}
	sourceDB, err := openAppDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceConfig := defaultConfig()
	sourceConfig.Addr = "127.0.0.1:9875"
	if err := saveConfigToDB(sourceDB, sourceConfig); err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}

	destinationPath := filepath.Join(tempDir, "program", appSlug+".db")
	destinationDB, err := openAppDB(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	defer destinationDB.Close()
	migrated, ok, err := migrateLegacyDBIfNeeded(destinationDB, destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || migrated.Addr != sourceConfig.Addr {
		t.Fatalf("user configuration DB migration = ok %t, addr %q", ok, migrated.Addr)
	}
}

func TestLegacyMigrationSkipsEmptyCandidate(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("APPDATA", filepath.Join(tempDir, "config"))
	t.Chdir(tempDir)

	emptyDB, err := openAppDB(legacyUserConfigDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := emptyDB.Close(); err != nil {
		t.Fatal(err)
	}

	sourceDB, err := openAppDB(filepath.Join(tempDir, appSlug+".db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceConfig := defaultConfig()
	sourceConfig.Addr = "127.0.0.1:9876"
	if err := saveConfigToDB(sourceDB, sourceConfig); err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}

	destinationPath := filepath.Join(tempDir, "destination", appSlug+".db")
	destinationDB, err := openAppDB(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	defer destinationDB.Close()
	migrated, ok, err := migrateLegacyDBIfNeeded(destinationDB, destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || migrated.Addr != sourceConfig.Addr {
		t.Fatalf("migration after empty candidate = ok %t, addr %q", ok, migrated.Addr)
	}
}

func TestDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", appSlug+".db")
	db, err := openAppDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("database mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestDownloadUpdateRejectsMissingPublisherSignature(t *testing.T) {
	payload := append([]byte("MZ"), bytes.Repeat([]byte{0x42}, 2048)...)
	digest := sha256.Sum256(payload)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousPublicKey := UpdatePublicKey
	UpdatePublicKey = base64.StdEncoding.EncodeToString(publicKey)
	t.Cleanup(func() { UpdatePublicKey = previousPublicKey })
	checksum := hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksum" {
			_, _ = io.WriteString(w, checksum+"  vision-relay.exe\n")
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	asset := githubAsset{Name: "vision-relay.exe", BrowserDownloadURL: server.URL, Size: int64(len(payload))}
	checksumAsset := githubAsset{Name: "vision-relay.exe.sha256", BrowserDownloadURL: server.URL + "/checksum"}
	info := updateInfo{AssetSize: asset.Size, asset: asset, release: githubRelease{Assets: []githubAsset{asset, checksumAsset}}}
	path, err := (&app{httpClient: server.Client()}).downloadUpdate(t.Context(), info, t.TempDir(), nil)
	if err == nil {
		_ = os.Remove(path)
		t.Fatal("download accepted a release without a publisher signature")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("missing signature error = %q", err)
	}
}

func TestDownloadUpdateRejectsMismatchedPublisherSignature(t *testing.T) {
	payload := append([]byte("MZ"), bytes.Repeat([]byte{0x24}, 2048)...)
	digest := sha256.Sum256(payload)
	trustedPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, untrustedPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousPublicKey := UpdatePublicKey
	UpdatePublicKey = base64.StdEncoding.EncodeToString(trustedPublicKey)
	t.Cleanup(func() { UpdatePublicKey = previousPublicKey })

	checksum := hex.EncodeToString(digest[:])
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(untrustedPrivateKey, digest[:]))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksum":
			_, _ = io.WriteString(w, checksum+"  vision-relay.exe\n")
		case "/signature":
			_, _ = io.WriteString(w, signature+"\n")
		default:
			_, _ = w.Write(payload)
		}
	}))
	defer server.Close()

	asset := githubAsset{Name: "vision-relay.exe", BrowserDownloadURL: server.URL, Size: int64(len(payload))}
	checksumAsset := githubAsset{Name: "vision-relay.exe.sha256", BrowserDownloadURL: server.URL + "/checksum"}
	signatureAsset := githubAsset{Name: "vision-relay.exe.sig", BrowserDownloadURL: server.URL + "/signature"}
	info := updateInfo{AssetSize: asset.Size, asset: asset, release: githubRelease{Assets: []githubAsset{asset, checksumAsset, signatureAsset}}}
	path, err := (&app{httpClient: server.Client()}).downloadUpdate(t.Context(), info, t.TempDir(), nil)
	if err == nil {
		_ = os.Remove(path)
		t.Fatal("download accepted an update signed by an untrusted key")
	}
	if !strings.Contains(err.Error(), "publisher signature verification failed") {
		t.Fatalf("mismatched signature error = %q", err)
	}
}
