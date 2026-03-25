package exoskeleton

import (
	"os"
	"strings"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	// Save and restore environment
	envVars := []string{
		"TENTACULAR_EXOSKELETON_ENABLED",
		"TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY",
		"TENTACULAR_POSTGRES_ADMIN_HOST",
		"TENTACULAR_POSTGRES_ADMIN_PORT",
		"TENTACULAR_POSTGRES_ADMIN_DATABASE",
		"TENTACULAR_POSTGRES_ADMIN_USER",
		"TENTACULAR_POSTGRES_ADMIN_PASSWORD",
		"TENTACULAR_NATS_URL",
		"TENTACULAR_NATS_TOKEN",
		"TENTACULAR_NATS_SPIFFE_ENABLED",
		"TENTACULAR_NATS_AUTHZ_CONFIGMAP",
		"TENTACULAR_NATS_AUTHZ_NAMESPACE",
		"TENTACULAR_RUSTFS_ENDPOINT",
		"TENTACULAR_RUSTFS_ACCESS_KEY",
		"TENTACULAR_RUSTFS_SECRET_KEY",
		"TENTACULAR_RUSTFS_BUCKET",
		"TENTACULAR_RUSTFS_REGION",
		"TENTACULAR_RUSTFS_CA_CERT_PATH",
		"TENTACULAR_RUSTFS_CA_CERT_PEM",
		"TENTACULAR_EXOSKELETON_AUTH_ENABLED",
		"TENTACULAR_KEYCLOAK_ISSUER",
		"TENTACULAR_KEYCLOAK_CLIENT_ID",
		"TENTACULAR_KEYCLOAK_CLIENT_SECRET",
		"TENTACULAR_EXOSKELETON_SPIRE_ENABLED",
		"TENTACULAR_SPIRE_CLASS_NAME",
	}
	saved := make(map[string]string)
	for _, k := range envVars {
		saved[k] = os.Getenv(k)
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	})

	// Clear all
	for _, k := range envVars {
		os.Unsetenv(k)
	}

	t.Run("disabled by default", func(t *testing.T) {
		cfg := LoadFromEnv()
		if cfg.Enabled {
			t.Error("expected Enabled=false when env not set")
		}
		if cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=false")
		}
		if cfg.NATSEnabled() {
			t.Error("expected NATSEnabled()=false")
		}
		if cfg.RustFSEnabled() {
			t.Error("expected RustFSEnabled()=false")
		}
	})

	t.Run("enabled with postgres", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "pg.local")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_USER", "admin")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "secret")

		cfg := LoadFromEnv()
		if !cfg.Enabled {
			t.Error("expected Enabled=true")
		}
		if !cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=true")
		}
		if cfg.Postgres.Port != "5432" {
			t.Errorf("expected default port 5432, got %q", cfg.Postgres.Port)
		}
		if cfg.Postgres.Database != "tentacular" {
			t.Errorf("expected default database tentacular, got %q", cfg.Postgres.Database)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_HOST")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_USER")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD")
	})

	t.Run("enabled with nats", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "1")
		os.Setenv("TENTACULAR_NATS_URL", "nats://localhost:4222")
		os.Setenv("TENTACULAR_NATS_TOKEN", "tok")

		cfg := LoadFromEnv()
		if !cfg.NATSEnabled() {
			t.Error("expected NATSEnabled()=true")
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_NATS_URL")
		os.Unsetenv("TENTACULAR_NATS_TOKEN")
	})

	t.Run("enabled with rustfs", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "yes")
		os.Setenv("TENTACULAR_RUSTFS_ENDPOINT", "http://minio:9000")
		os.Setenv("TENTACULAR_RUSTFS_ACCESS_KEY", "ak")
		os.Setenv("TENTACULAR_RUSTFS_SECRET_KEY", "sk")
		os.Setenv("TENTACULAR_RUSTFS_CA_CERT_PATH", "/etc/ssl/ca.pem")
		os.Setenv("TENTACULAR_RUSTFS_CA_CERT_PEM", "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")

		cfg := LoadFromEnv()
		if !cfg.RustFSEnabled() {
			t.Error("expected RustFSEnabled()=true")
		}
		if cfg.RustFS.Bucket != "tentacular" {
			t.Errorf("expected default bucket tentacular, got %q", cfg.RustFS.Bucket)
		}
		if cfg.RustFS.Region != "us-east-1" {
			t.Errorf("expected default region us-east-1, got %q", cfg.RustFS.Region)
		}
		if cfg.RustFS.CACertPath != "/etc/ssl/ca.pem" {
			t.Errorf("expected CACertPath, got %q", cfg.RustFS.CACertPath)
		}
		if cfg.RustFS.CACertPEM == "" {
			t.Error("expected CACertPEM to be set")
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_RUSTFS_ENDPOINT")
		os.Unsetenv("TENTACULAR_RUSTFS_ACCESS_KEY")
		os.Unsetenv("TENTACULAR_RUSTFS_SECRET_KEY")
		os.Unsetenv("TENTACULAR_RUSTFS_CA_CERT_PATH")
		os.Unsetenv("TENTACULAR_RUSTFS_CA_CERT_PEM")
	})

	t.Run("cleanup on undeploy", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY", "true")
		cfg := LoadFromEnv()
		if !cfg.CleanupOnUndeploy {
			t.Error("expected CleanupOnUndeploy=true")
		}
		os.Unsetenv("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY")
	})

	t.Run("auth disabled by default", func(t *testing.T) {
		cfg := LoadFromEnv()
		if cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=false when env not set")
		}
	})

	t.Run("auth enabled with config", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED", "true")
		os.Setenv("TENTACULAR_KEYCLOAK_ISSUER", "http://keycloak.local/realms/test")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_ID", "tentacular-mcp")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET", "secret123")

		cfg := LoadFromEnv()
		if !cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=true")
		}
		if cfg.Auth.IssuerURL != "http://keycloak.local/realms/test" {
			t.Errorf("expected issuer URL, got %q", cfg.Auth.IssuerURL)
		}
		if cfg.Auth.ClientID != "tentacular-mcp" {
			t.Errorf("expected client ID, got %q", cfg.Auth.ClientID)
		}
		if cfg.Auth.ClientSecret != "secret123" {
			t.Errorf("expected client secret, got %q", cfg.Auth.ClientSecret)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED")
		os.Unsetenv("TENTACULAR_KEYCLOAK_ISSUER")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_ID")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET")
	})

	t.Run("auth enabled but missing issuer", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED", "true")
		os.Setenv("TENTACULAR_KEYCLOAK_CLIENT_ID", "tentacular-mcp")

		cfg := LoadFromEnv()
		if cfg.AuthEnabled() {
			t.Error("expected AuthEnabled()=false when issuer is missing")
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_AUTH_ENABLED")
		os.Unsetenv("TENTACULAR_KEYCLOAK_CLIENT_ID")
	})

	t.Run("spire disabled by default", func(t *testing.T) {
		cfg := LoadFromEnv()
		if cfg.SPIREEnabled() {
			t.Error("expected SPIREEnabled()=false when env not set")
		}
	})

	t.Run("spire enabled", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_EXOSKELETON_SPIRE_ENABLED", "true")

		cfg := LoadFromEnv()
		if !cfg.SPIREEnabled() {
			t.Error("expected SPIREEnabled()=true")
		}
		if cfg.SPIRE.ClassName != "tentacular-system-spire" {
			t.Errorf("expected default class name, got %q", cfg.SPIRE.ClassName)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_EXOSKELETON_SPIRE_ENABLED")
	})

	t.Run("spire custom class name", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_EXOSKELETON_SPIRE_ENABLED", "true")
		os.Setenv("TENTACULAR_SPIRE_CLASS_NAME", "custom-spire")

		cfg := LoadFromEnv()
		if cfg.SPIRE.ClassName != "custom-spire" {
			t.Errorf("expected custom class name, got %q", cfg.SPIRE.ClassName)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_EXOSKELETON_SPIRE_ENABLED")
		os.Unsetenv("TENTACULAR_SPIRE_CLASS_NAME")
	})

	t.Run("nats spiffe defaults", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_NATS_URL", "nats://localhost:4222")

		cfg := LoadFromEnv()
		if cfg.NATS.SPIFFEEnabled {
			t.Error("expected SPIFFEEnabled=false by default")
		}
		if cfg.NATS.AuthzConfigMap != "nats-tentacular-authz" {
			t.Errorf("expected default AuthzConfigMap, got %q", cfg.NATS.AuthzConfigMap)
		}
		if cfg.NATS.AuthzNamespace != "tentacular-exoskeleton" {
			t.Errorf("expected default AuthzNamespace, got %q", cfg.NATS.AuthzNamespace)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_NATS_URL")
	})

	t.Run("nats spiffe enabled with custom configmap", func(t *testing.T) {
		os.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
		os.Setenv("TENTACULAR_NATS_URL", "nats://localhost:4222")
		os.Setenv("TENTACULAR_NATS_SPIFFE_ENABLED", "true")
		os.Setenv("TENTACULAR_NATS_AUTHZ_CONFIGMAP", "custom-authz")
		os.Setenv("TENTACULAR_NATS_AUTHZ_NAMESPACE", "custom-ns")

		cfg := LoadFromEnv()
		if !cfg.NATS.SPIFFEEnabled {
			t.Error("expected SPIFFEEnabled=true")
		}
		if cfg.NATS.AuthzConfigMap != "custom-authz" {
			t.Errorf("expected custom AuthzConfigMap, got %q", cfg.NATS.AuthzConfigMap)
		}
		if cfg.NATS.AuthzNamespace != "custom-ns" {
			t.Errorf("expected custom AuthzNamespace, got %q", cfg.NATS.AuthzNamespace)
		}

		os.Unsetenv("TENTACULAR_EXOSKELETON_ENABLED")
		os.Unsetenv("TENTACULAR_NATS_URL")
		os.Unsetenv("TENTACULAR_NATS_SPIFFE_ENABLED")
		os.Unsetenv("TENTACULAR_NATS_AUTHZ_CONFIGMAP")
		os.Unsetenv("TENTACULAR_NATS_AUTHZ_NAMESPACE")
	})

	t.Run("not enabled without top flag", func(t *testing.T) {
		// Postgres creds present but top-level flag off
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "pg.local")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_USER", "admin")
		os.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "secret")

		cfg := LoadFromEnv()
		if cfg.PostgresEnabled() {
			t.Error("expected PostgresEnabled()=false when top-level disabled")
		}

		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_HOST")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_USER")
		os.Unsetenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD")
	})
}

func TestValidateDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on disabled config should not error: %v", err)
	}
}

func TestValidateFullyConfigured(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Postgres: PostgresConfig{
			Host: "pg.local", User: "admin", Password: "secret",
		},
		NATS: NATSConfig{URL: "nats://localhost:4222", Token: "tok"},
		RustFS: RustFSConfig{
			Endpoint: "http://minio:9000", AccessKey: "ak", SecretKey: "sk",
		},
		Auth: AuthConfig{
			Enabled: true, IssuerURL: "http://keycloak/realms/test", ClientID: "mcp",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on fully configured should pass: %v", err)
	}
}

func TestValidatePartialPostgres(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Postgres: PostgresConfig{Host: "pg.local"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for partial postgres config")
	}
	if !strings.Contains(err.Error(), "postgres partially configured") {
		t.Errorf("expected postgres partial message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "TENTACULAR_POSTGRES_ADMIN_USER") {
		t.Errorf("expected missing user mentioned, got: %v", err)
	}
	if !strings.Contains(err.Error(), "TENTACULAR_POSTGRES_ADMIN_PASSWORD") {
		t.Errorf("expected missing password mentioned, got: %v", err)
	}
}

func TestValidateNATSNoAuth(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		NATS:    NATSConfig{URL: "nats://localhost:4222"},
	}
	// NATS without any auth method (no token, no SPIFFE) must be rejected.
	// The registrar supports only two modes: token and SPIFFE mTLS.
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for NATS URL without auth method")
	}
	if !strings.Contains(err.Error(), "nats URL configured but no auth method") {
		t.Errorf("expected nats auth message, got: %v", err)
	}
}

func TestValidateNATSWithSPIFFE(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		NATS:    NATSConfig{URL: "nats://localhost:4222", SPIFFEEnabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("NATS with SPIFFE should pass: %v", err)
	}
}

func TestValidatePartialRustFS(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		RustFS:  RustFSConfig{Endpoint: "http://minio:9000"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for partial rustfs config")
	}
	if !strings.Contains(err.Error(), "rustfs partially configured") {
		t.Errorf("expected rustfs partial message, got: %v", err)
	}
}

func TestValidateAuthEnabledMissingIssuer(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Auth:    AuthConfig{Enabled: true, ClientID: "mcp"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for auth enabled but missing issuer")
	}
	if !strings.Contains(err.Error(), "auth enabled but missing") {
		t.Errorf("expected auth message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "TENTACULAR_KEYCLOAK_ISSUER") {
		t.Errorf("expected issuer mentioned, got: %v", err)
	}
}

func TestValidateMultipleProblems(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Postgres: PostgresConfig{Host: "pg.local"},
		RustFS:   RustFSConfig{Endpoint: "http://minio:9000"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for multiple problems")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("expected postgres mentioned: %v", err)
	}
	if !strings.Contains(err.Error(), "rustfs") {
		t.Errorf("expected rustfs mentioned: %v", err)
	}
}
