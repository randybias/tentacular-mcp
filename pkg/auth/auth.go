package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

type contextKey string

// DeployerContextKey is the context key used to store DeployerInfo.
const DeployerContextKey contextKey = "deployer"

// DeployerFromContext extracts the DeployerInfo from the request context.
// Returns nil if no deployer info is present (e.g., bearer-token auth
// without OIDC, or OIDC disabled).
func DeployerFromContext(ctx context.Context) *exoskeleton.DeployerInfo {
	if v := ctx.Value(DeployerContextKey); v != nil {
		if di, ok := v.(*exoskeleton.DeployerInfo); ok {
			return di
		}
	}
	return nil
}

// LoadToken reads the bearer token from the given file path.
func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path comes from server configuration, not user input
	if err != nil {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

// writeUnauthorized writes a 401 response with an optional WWW-Authenticate header.
// When resourceMetadataURL is non-empty, the header points clients to the
// RFC 9728 Protected Resource Metadata endpoint for auth server discovery.
func writeUnauthorized(w http.ResponseWriter, resourceMetadataURL, msg string) {
	if resourceMetadataURL != "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, resourceMetadataURL))
	}
	http.Error(w, msg, http.StatusUnauthorized)
}

// bypassAuth returns true for paths that should skip authentication.
func bypassAuth(path string) bool {
	return path == "/healthz" || path == "/.well-known/oauth-protected-resource"
}

// Middleware returns an HTTP middleware that validates Bearer token authentication.
// The /healthz and /.well-known/oauth-protected-resource endpoints bypass authentication.
// resourceMetadataURL is the URL to the RFC 9728 metadata endpoint; pass "" to omit
// the WWW-Authenticate header on 401 responses.
func Middleware(token, resourceMetadataURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bypassAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeUnauthorized(w, resourceMetadataURL, "missing authorization header")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeUnauthorized(w, resourceMetadataURL, "invalid authorization header format")
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeUnauthorized(w, resourceMetadataURL, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// DualAuthMiddleware returns an HTTP middleware that supports both OIDC token
// validation and bearer-token authentication.
//
// When validator is non-nil (OIDC enabled):
//  1. Extract the Bearer token from the Authorization header.
//  2. If it looks like a JWT (contains dots), try OIDC validation first.
//     On success, attach DeployerInfo to the request context.
//  3. If OIDC validation fails or the token is not a JWT, fall back to
//     bearer-token comparison.
//
// When validator is nil (OIDC disabled):
//
//	Behaves identically to the original Middleware (bearer-token only).
//
// The /healthz and /.well-known/oauth-protected-resource endpoints always bypass
// authentication. resourceMetadataURL is the URL to the RFC 9728 metadata endpoint;
// pass "" to omit the WWW-Authenticate header on 401 responses.
func DualAuthMiddleware(token string, validator *exoskeleton.OIDCValidator, resourceMetadataURL string, next http.Handler) http.Handler {
	// If no OIDC validator, use the simple bearer-token middleware.
	if validator == nil {
		return Middleware(token, resourceMetadataURL, next)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bypassAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeUnauthorized(w, resourceMetadataURL, "missing authorization header")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeUnauthorized(w, resourceMetadataURL, "invalid authorization header format")
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")

		// Try OIDC validation if the token looks like a JWT (has dot-separated parts).
		if looksLikeJWT(provided) {
			deployer, err := validator.ValidateToken(r.Context(), provided)
			if err == nil {
				// OIDC token is valid. Attach deployer info and proceed.
				ctx := context.WithValue(r.Context(), DeployerContextKey, deployer)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// OIDC validation failed. Log at debug level and fall through
			// to bearer-token check. This handles the case where a hex
			// bearer token happens to contain dots (unlikely but safe).
			slog.Debug("OIDC token validation failed, falling back to bearer token", "error", err)
		}

		// Bearer-token fallback: constant-time comparison.
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeUnauthorized(w, resourceMetadataURL, "invalid token")
			return
		}

		// Bearer token is valid. Attach a minimal deployer with "bearer-token" provider.
		deployer := &exoskeleton.DeployerInfo{
			Provider: "bearer-token",
		}
		ctx := context.WithValue(r.Context(), DeployerContextKey, deployer)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// looksLikeJWT returns true if the token string has the structure of a JWT
// (three base64url-encoded segments separated by dots).
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && len(parts[0]) > 0 && len(parts[1]) > 0 && len(parts[2]) > 0
}
