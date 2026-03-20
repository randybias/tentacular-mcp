package exoskeleton

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const testKeyID = "test-key-1"

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

	// Placeholder: the issuer URL will be set after server starts.
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

// signToken creates a signed JWT with the given claims using the provided key.
func signToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	signerOpts := jose.SignerOptions{}
	signerOpts.WithHeader(jose.HeaderKey("kid"), testKeyID)
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

func TestOIDCValidator_ValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := NewOIDCValidator(AuthConfig{
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
		"sub":               "user-123",
		"email":             "user@example.com",
		"name":              "Test User",
		"azp":               "tentacular-mcp",
		"identity_provider": "google",
		"iat":               jwt.NewNumericDate(now),
		"exp":               jwt.NewNumericDate(now.Add(time.Hour)),
	}

	token := signToken(t, key, claims)

	deployer, err := validator.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}

	if deployer.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %q", deployer.Email)
	}
	if deployer.DisplayName != "Test User" {
		t.Errorf("expected display name 'Test User', got %q", deployer.DisplayName)
	}
	if deployer.Subject != "user-123" {
		t.Errorf("expected subject user-123, got %q", deployer.Subject)
	}
	if deployer.Provider != "google" {
		t.Errorf("expected provider google, got %q", deployer.Provider)
	}
}

func TestOIDCValidator_ExpiredToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := NewOIDCValidator(AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	past := time.Now().Add(-2 * time.Hour)
	claims := map[string]any{
		"iss": srv.URL,
		"aud": "tentacular-mcp",
		"sub": "user-123",
		"iat": jwt.NewNumericDate(past),
		"exp": jwt.NewNumericDate(past.Add(time.Hour)), // expired 1 hour ago
	}

	token := signToken(t, key, claims)

	_, err = validator.ValidateToken(context.Background(), token)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestOIDCValidator_WrongAudience(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := NewOIDCValidator(AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	now := time.Now()
	claims := map[string]any{
		"iss": srv.URL,
		"aud": "wrong-client",
		"sub": "user-123",
		"iat": jwt.NewNumericDate(now),
		"exp": jwt.NewNumericDate(now.Add(time.Hour)),
	}

	token := signToken(t, key, claims)

	_, err = validator.ValidateToken(context.Background(), token)
	if err == nil {
		t.Error("expected error for wrong audience, got nil")
	}
}

func TestOIDCValidator_KeycloakProviderFallback(t *testing.T) {
	// When identity_provider is not set and azp matches clientID, default to "keycloak"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv := testKeycloakServer(t, key)
	defer srv.Close()

	validator, err := NewOIDCValidator(AuthConfig{
		Enabled:   true,
		IssuerURL: srv.URL,
		ClientID:  "tentacular-mcp",
	})
	if err != nil {
		t.Fatalf("create validator: %v", err)
	}

	now := time.Now()
	claims := map[string]any{
		"iss":   srv.URL,
		"aud":   "tentacular-mcp",
		"sub":   "user-456",
		"email": "keycloak-user@example.com",
		"azp":   "tentacular-mcp",
		"iat":   jwt.NewNumericDate(now),
		"exp":   jwt.NewNumericDate(now.Add(time.Hour)),
	}

	token := signToken(t, key, claims)

	deployer, err := validator.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}

	if deployer.Provider != "keycloak" {
		t.Errorf("expected provider keycloak, got %q", deployer.Provider)
	}
}

func TestNewOIDCValidator_DisabledConfig(t *testing.T) {
	_, err := NewOIDCValidator(AuthConfig{Enabled: false})
	if err == nil {
		t.Error("expected error when auth is disabled")
	}
}

func TestNewOIDCValidator_MissingIssuer(t *testing.T) {
	_, err := NewOIDCValidator(AuthConfig{Enabled: true, ClientID: "test"})
	if err == nil {
		t.Error("expected error when issuer URL is missing")
	}
}

func TestNewOIDCValidator_MissingClientID(t *testing.T) {
	_, err := NewOIDCValidator(AuthConfig{Enabled: true, IssuerURL: "http://example.com"})
	if err == nil {
		t.Error("expected error when client ID is missing")
	}
}

func TestDetermineProvider(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		claims   keycloakClaims
	}{
		{
			name:     "identity_provider set",
			expected: "google",
			claims:   keycloakClaims{IdentityProvider: "google"},
		},
		{
			name:     "azp differs from our client",
			expected: "some-other-client",
			claims:   keycloakClaims{AZP: "some-other-client"},
		},
		{
			name:     "azp matches our client",
			expected: "keycloak",
			claims:   keycloakClaims{AZP: "tentacular-mcp"},
		},
		{
			name:     "no identity info",
			expected: "keycloak",
			claims:   keycloakClaims{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineProvider(tt.claims)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
