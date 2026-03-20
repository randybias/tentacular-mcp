package exoskeleton

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/coreos/go-oidc/v3/oidc"
)

// AuthConfig holds OIDC/Keycloak authentication configuration.
type AuthConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Enabled      bool
}

// DeployerInfo contains identity information extracted from an OIDC token
// or synthesized from a bearer-token auth path.
type DeployerInfo struct {
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Subject     string   `json:"subject"`
	Provider    string   `json:"provider"`   // "google", "keycloak", "bearer-token"
	AgentType   string   `json:"agent_type"` // e.g. "claude-code", "mcp-client"
	SessionID   string   `json:"session_id"`
	Groups      []string `json:"groups,omitempty"` // IdP group memberships (live from JWT)
}

// OIDCValidator validates OIDC tokens using JWKS fetched from the issuer's
// discovery endpoint.
type OIDCValidator struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	clientID string
}

// NewOIDCValidator creates a validator that fetches JWKS from the issuer and
// validates tokens against the configured client ID.
func NewOIDCValidator(cfg AuthConfig) (*OIDCValidator, error) {
	if !cfg.Enabled {
		return nil, errors.New("OIDC auth is not enabled")
	}
	if cfg.IssuerURL == "" {
		return nil, errors.New("OIDC issuer URL is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("OIDC client ID is required")
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC provider discovery for %s: %w", cfg.IssuerURL, err)
	}

	// Keycloak access tokens use "azp" (authorized party) instead of "aud"
	// for the client ID. Skip the default audience check and validate azp
	// manually in ValidateToken.
	verifier := provider.Verifier(&oidc.Config{
		SkipClientIDCheck: true,
	})

	slog.Info("OIDC validator initialized", "issuer", cfg.IssuerURL, "client_id", cfg.ClientID)

	return &OIDCValidator{
		provider: provider,
		verifier: verifier,
		clientID: cfg.ClientID,
	}, nil
}

// keycloakClaims holds the claims we extract from a Keycloak OIDC token.
type keycloakClaims struct {
	Email            string   `json:"email"`
	Name             string   `json:"name"`
	PreferredUser    string   `json:"preferred_username"`
	AZP              string   `json:"azp"`
	IdentityProvider string   `json:"identity_provider"`
	Groups           []string `json:"groups"` // Keycloak group membership claim
}

// ValidateToken verifies the token signature, expiry, audience, and issuer,
// then extracts deployer identity from the claims.
func (v *OIDCValidator) ValidateToken(ctx context.Context, tokenString string) (*DeployerInfo, error) {
	idToken, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		return nil, fmt.Errorf("OIDC token verification failed: %w", err)
	}

	var claims keycloakClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("OIDC claims extraction failed: %w", err)
	}

	// Validate azp (authorized party) since Keycloak access tokens use azp
	// instead of aud for the client identifier.
	if claims.AZP != v.clientID {
		return nil, fmt.Errorf("OIDC token azp %q does not match client ID %q", claims.AZP, v.clientID)
	}

	provider := determineProvider(claims)

	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUser
	}

	slog.Info("OIDC token validated", "email", claims.Email, "subject", idToken.Subject, "provider", provider, "groups", len(claims.Groups))

	groups := claims.Groups
	if groups == nil {
		groups = []string{}
	}

	return &DeployerInfo{
		Email:       claims.Email,
		DisplayName: displayName,
		Subject:     idToken.Subject,
		Provider:    provider,
		Groups:      groups,
	}, nil
}

// determineProvider infers the identity provider from the token claims.
// Keycloak sets "identity_provider" when brokering (e.g., "google").
// Otherwise we check the authorized party (azp) or default to "keycloak".
func determineProvider(claims keycloakClaims) string {
	if claims.IdentityProvider != "" {
		return claims.IdentityProvider
	}
	// If azp looks like a Google client ID, it's likely Google SSO
	if claims.AZP != "" && claims.AZP != "tentacular-mcp" {
		return claims.AZP
	}
	return "keycloak"
}
