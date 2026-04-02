package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

const (
	testToken               = "super-secret-token"
	testKeyID               = "test-key-1"
	testResourceMetadataURL = "https://mcp.example.com/.well-known/oauth-protected-resource"
)

// okHandler is a trivial HTTP handler that returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func makeRequest(t *testing.T, handler http.Handler, path, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestMiddleware_ValidToken(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/some/path", "Bearer "+testToken)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_MissingToken(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/some/path", "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/some/path", "Bearer wrong-token")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestMiddleware_MissingBearerPrefix(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/some/path", testToken)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing Bearer prefix, got %d", rr.Code)
	}
}

func TestMiddleware_HealthzBypassesAuth(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	// No Authorization header on /healthz should still succeed.
	rr := makeRequest(t, h, "/healthz", "")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for /healthz without auth, got %d", rr.Code)
	}
}

func TestMiddleware_WWWAuthenticate(t *testing.T) {
	h := auth.Middleware(testToken, testResourceMetadataURL, okHandler)
	rr := makeRequest(t, h, "/mcp", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Fatal("expected WWW-Authenticate header, got empty")
	}
	expected := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`
	if wwwAuth != expected {
		t.Errorf("WWW-Authenticate header mismatch\n  got:  %s\n  want: %s", wwwAuth, expected)
	}
}

func TestMiddleware_NoWWWAuthenticateWithoutMetadataURL(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/mcp", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if wwwAuth := rr.Header().Get("WWW-Authenticate"); wwwAuth != "" {
		t.Errorf("expected no WWW-Authenticate header, got %q", wwwAuth)
	}
}

func TestMiddleware_WellKnownBypassesAuth(t *testing.T) {
	h := auth.Middleware(testToken, "", okHandler)
	rr := makeRequest(t, h, "/.well-known/oauth-protected-resource", "")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for well-known endpoint without auth, got %d", rr.Code)
	}
}

func TestLoadToken_Success(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("  mytoken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := auth.LoadToken(tokenFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "mytoken" {
		t.Errorf("expected trimmed token 'mytoken', got %q", tok)
	}
}

func TestLoadToken_FileMissing(t *testing.T) {
	_, err := auth.LoadToken("/nonexistent/path/token")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadToken_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "empty-token")
	if err := os.WriteFile(tokenFile, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := auth.LoadToken(tokenFile)
	if err == nil {
		t.Error("expected error for empty token file, got nil")
	}
}

// --- Dual Auth Middleware Tests ---

// testKeycloakServer creates an httptest.Server that serves OIDC discovery
// and JWKS endpoints using the given RSA key.
func testKeycloakServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()

	jwk := jose.JSONWebKey{
		Key:       &key.PublicKey,
		KeyID:     testKeyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	mux := http.NewServeMux()
	var issuerURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discovery := map[string]any{
			"issuer":                                issuerURL,
			"authorization_endpoint":                issuerURL + "/protocol/openid-connect/auth",
			"token_endpoint":                        issuerURL + "/protocol/openid-connect/token",
			"jwks_uri":                              issuerURL + "/protocol/openid-connect/certs",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(discovery)
	})

	mux.HandleFunc("/protocol/openid-connect/certs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	issuerURL = srv.URL
	return srv
}

// signTestToken creates a signed JWT for testing.
func signTestToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()

	signerOpts := jose.SignerOptions{}
	signerOpts.WithHeader(jose.HeaderKey("kid"), keyID)
	signerOpts.WithType("JWT")

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, &signerOpts)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	serialized, err := jws.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize token: %v", err)
	}
	return serialized
}

func TestDualAuthMiddleware_NilValidator_BearerToken(t *testing.T) {
	// When validator is nil, should behave like basic Middleware.
	h := auth.DualAuthMiddleware(testToken, nil, "", okHandler)
	rr := makeRequest(t, h, "/some/path", "Bearer "+testToken)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestDualAuthMiddleware_NilValidator_InvalidToken(t *testing.T) {
	h := auth.DualAuthMiddleware(testToken, nil, "", okHandler)
	rr := makeRequest(t, h, "/some/path", "Bearer wrong")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestDualAuthMiddleware_OIDCToken_SetsDeployerContext(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	now := time.Now()
	claims := map[string]any{
		"iss":               srv.URL,
		"aud":               "tentacular-mcp",
		"azp":               "tentacular-mcp",
		"sub":               "user-123",
		"email":             "test@example.com",
		"name":              "Test User",
		"identity_provider": "google",
		"iat":               jwt.NewNumericDate(now),
		"exp":               jwt.NewNumericDate(now.Add(time.Hour)),
	}
	token := signTestToken(t, key, testKeyID, claims)

	// Handler that checks for DeployerInfo in context.
	var gotDeployer *exoskeleton.DeployerInfo
	checkHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeployer = auth.DeployerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	h := auth.DualAuthMiddleware(testToken, validator, "", checkHandler)
	rr := makeRequest(t, h, "/mcp", "Bearer "+token)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotDeployer == nil {
		t.Fatal("expected deployer info in context, got nil")
	}
	if gotDeployer.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %q", gotDeployer.Email)
	}
	if gotDeployer.Provider != "google" {
		t.Errorf("expected provider google, got %q", gotDeployer.Provider)
	}
}

func TestDualAuthMiddleware_BearerFallback_WithValidator(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	// Use a hex bearer token (not a JWT) — should fall through to bearer validation.
	var gotDeployer *exoskeleton.DeployerInfo
	checkHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeployer = auth.DeployerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	h := auth.DualAuthMiddleware(testToken, validator, "", checkHandler)
	rr := makeRequest(t, h, "/mcp", "Bearer "+testToken)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotDeployer == nil {
		t.Fatal("expected deployer info in context for bearer token, got nil")
	}
	if gotDeployer.Provider != "bearer-token" {
		t.Errorf("expected provider bearer-token, got %q", gotDeployer.Provider)
	}
}

func TestDualAuthMiddleware_HealthzBypass(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	h := auth.DualAuthMiddleware(testToken, validator, "", okHandler)
	rr := makeRequest(t, h, "/healthz", "")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for /healthz, got %d", rr.Code)
	}
}

func TestDualAuthMiddleware_WellKnownBypass(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	h := auth.DualAuthMiddleware(testToken, validator, "", okHandler)
	rr := makeRequest(t, h, "/.well-known/oauth-protected-resource", "")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for well-known endpoint, got %d", rr.Code)
	}
}

func TestDualAuthMiddleware_WWWAuthenticate(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	h := auth.DualAuthMiddleware(testToken, validator, testResourceMetadataURL, okHandler)
	rr := makeRequest(t, h, "/mcp", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Fatal("expected WWW-Authenticate header on 401, got empty")
	}
	expected := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`
	if wwwAuth != expected {
		t.Errorf("WWW-Authenticate header mismatch\n  got:  %s\n  want: %s", wwwAuth, expected)
	}
}

func TestDualAuthMiddleware_InvalidBothPaths(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := exoskeleton.NewOIDCValidator(exoskeleton.AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	h := auth.DualAuthMiddleware(testToken, validator, "", okHandler)
	rr := makeRequest(t, h, "/mcp", "Bearer totally-wrong-token")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestDeployerFromContext_Nil(t *testing.T) {
	ctx := context.Background()
	if d := auth.DeployerFromContext(ctx); d != nil {
		t.Errorf("expected nil deployer from empty context, got %+v", d)
	}
}

func TestDeployerFromContext_WithValue(t *testing.T) {
	deployer := &exoskeleton.DeployerInfo{
		Email:   "test@example.com",
		Subject: "sub-1",
	}
	ctx := context.WithValue(context.Background(), auth.DeployerContextKey, deployer)
	got := auth.DeployerFromContext(ctx)
	if got == nil {
		t.Fatal("expected deployer, got nil")
	}
	if got.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %q", got.Email)
	}
}
