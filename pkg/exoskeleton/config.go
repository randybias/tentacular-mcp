package exoskeleton

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all exoskeleton configuration, loaded from environment variables.
type Config struct {
	Postgres          PostgresConfig
	RustFS            RustFSConfig
	NATS              NATSConfig
	Auth              AuthConfig
	SPIRE             SPIREConfig
	Enabled           bool
	CleanupOnUndeploy bool
}

// SPIREConfig holds SPIRE identity registration configuration.
type SPIREConfig struct {
	ClassName string
	Enabled   bool
}

// PostgresConfig holds admin connection details for the Postgres registrar.
type PostgresConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	SSLMode  string
}

// NATSConfig holds connection details for the NATS registrar.
type NATSConfig struct {
	URL            string
	Token          string
	AuthzConfigMap string
	AuthzNamespace string
	SPIFFEEnabled  bool
}

// RustFSConfig holds admin connection details for the RustFS (MinIO-compatible) registrar.
type RustFSConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	Bucket     string
	Region     string
	CACertPath string
}

// LoadFromEnv reads exoskeleton configuration from environment variables.
func LoadFromEnv() *Config {
	return &Config{
		Enabled:           envBool("TENTACULAR_EXOSKELETON_ENABLED"),
		CleanupOnUndeploy: envBool("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY"),
		Postgres: PostgresConfig{
			Host:     os.Getenv("TENTACULAR_POSTGRES_ADMIN_HOST"),
			Port:     envDefault("TENTACULAR_POSTGRES_ADMIN_PORT", "5432"),
			Database: envDefault("TENTACULAR_POSTGRES_ADMIN_DATABASE", "tentacular"),
			User:     os.Getenv("TENTACULAR_POSTGRES_ADMIN_USER"),
			Password: os.Getenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD"),
			SSLMode:  envDefault("TENTACULAR_POSTGRES_SSLMODE", "disable"),
		},
		NATS: NATSConfig{
			URL:            os.Getenv("TENTACULAR_NATS_URL"),
			Token:          os.Getenv("TENTACULAR_NATS_TOKEN"),
			SPIFFEEnabled:  envBool("TENTACULAR_NATS_SPIFFE_ENABLED"),
			AuthzConfigMap: envDefault("TENTACULAR_NATS_AUTHZ_CONFIGMAP", "nats-tentacular-authz"),
			AuthzNamespace: envDefault("TENTACULAR_NATS_AUTHZ_NAMESPACE", "tentacular-exoskeleton"),
		},
		RustFS: RustFSConfig{
			Endpoint:   os.Getenv("TENTACULAR_RUSTFS_ENDPOINT"),
			AccessKey:  os.Getenv("TENTACULAR_RUSTFS_ACCESS_KEY"),
			SecretKey:  os.Getenv("TENTACULAR_RUSTFS_SECRET_KEY"),
			Bucket:     envDefault("TENTACULAR_RUSTFS_BUCKET", "tentacular"),
			Region:     envDefault("TENTACULAR_RUSTFS_REGION", "us-east-1"),
			CACertPath: os.Getenv("TENTACULAR_RUSTFS_CA_CERT_PATH"),
		},
		Auth: AuthConfig{
			Enabled:      envBool("TENTACULAR_EXOSKELETON_AUTH_ENABLED"),
			IssuerURL:    os.Getenv("TENTACULAR_KEYCLOAK_ISSUER"),
			ClientID:     os.Getenv("TENTACULAR_KEYCLOAK_CLIENT_ID"),
			ClientSecret: os.Getenv("TENTACULAR_KEYCLOAK_CLIENT_SECRET"),
		},
		SPIRE: SPIREConfig{
			Enabled:   envBool("TENTACULAR_EXOSKELETON_SPIRE_ENABLED"),
			ClassName: envDefault("TENTACULAR_SPIRE_CLASS_NAME", "tentacular-system-spire"),
		},
	}
}

// PostgresEnabled returns true when exoskeleton is enabled and Postgres
// admin credentials are configured.
func (c *Config) PostgresEnabled() bool {
	return c.Enabled && c.Postgres.Host != "" && c.Postgres.User != "" && c.Postgres.Password != ""
}

// NATSEnabled returns true when exoskeleton is enabled and the NATS URL
// is configured.
func (c *Config) NATSEnabled() bool {
	return c.Enabled && c.NATS.URL != ""
}

// RustFSEnabled returns true when exoskeleton is enabled and the RustFS
// endpoint and credentials are configured.
func (c *Config) RustFSEnabled() bool {
	return c.Enabled && c.RustFS.Endpoint != "" && c.RustFS.AccessKey != "" && c.RustFS.SecretKey != ""
}

// AuthEnabled returns true when exoskeleton auth is enabled and the
// required OIDC configuration is present.
func (c *Config) AuthEnabled() bool {
	return c.Auth.Enabled && c.Auth.IssuerURL != "" && c.Auth.ClientID != ""
}

// SPIREEnabled returns true when exoskeleton is enabled and SPIRE
// identity registration is enabled.
func (c *Config) SPIREEnabled() bool {
	return c.Enabled && c.SPIRE.Enabled
}

// Validate checks for likely misconfiguration: a service appears partially
// configured (some fields set, but not enough for the *Enabled() check to
// pass). Returns an error listing every problem found. When the exoskeleton
// is disabled, validation is skipped.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	var problems []string

	// Postgres: host or user set but not enough for PostgresEnabled()
	pgPartial := c.Postgres.Host != "" || c.Postgres.User != "" || c.Postgres.Password != ""
	if pgPartial && !c.PostgresEnabled() {
		var missing []string
		if c.Postgres.Host == "" {
			missing = append(missing, "TENTACULAR_POSTGRES_ADMIN_HOST")
		}
		if c.Postgres.User == "" {
			missing = append(missing, "TENTACULAR_POSTGRES_ADMIN_USER")
		}
		if c.Postgres.Password == "" {
			missing = append(missing, "TENTACULAR_POSTGRES_ADMIN_PASSWORD")
		}
		problems = append(problems, "postgres partially configured, missing: "+strings.Join(missing, ", "))
	}

	// NATS: URL set but no auth method configured — this is an error.
	// The NATS registrar supports two auth modes: token and SPIFFE mTLS.
	// Anonymous connections are not permitted.
	if c.NATS.URL != "" && !c.NATS.SPIFFEEnabled && c.NATS.Token == "" {
		problems = append(problems, "nats URL configured but no auth method set; provide TENTACULAR_NATS_TOKEN or enable TENTACULAR_NATS_SPIFFE_ENABLED")
	}

	// RustFS: some fields set but not enough for RustFSEnabled()
	rustPartial := c.RustFS.Endpoint != "" || c.RustFS.AccessKey != "" || c.RustFS.SecretKey != ""
	if rustPartial && !c.RustFSEnabled() {
		var missing []string
		if c.RustFS.Endpoint == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_ENDPOINT")
		}
		if c.RustFS.AccessKey == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_ACCESS_KEY")
		}
		if c.RustFS.SecretKey == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_SECRET_KEY")
		}
		problems = append(problems, "rustfs partially configured, missing: "+strings.Join(missing, ", "))
	}

	// Auth: enabled flag set but missing required fields
	if c.Auth.Enabled && !c.AuthEnabled() {
		var missing []string
		if c.Auth.IssuerURL == "" {
			missing = append(missing, "TENTACULAR_KEYCLOAK_ISSUER")
		}
		if c.Auth.ClientID == "" {
			missing = append(missing, "TENTACULAR_KEYCLOAK_CLIENT_ID")
		}
		problems = append(problems, "auth enabled but missing: "+strings.Join(missing, ", "))
	}

	if len(problems) > 0 {
		return fmt.Errorf("exoskeleton config: %s", strings.Join(problems, "; "))
	}

	return nil
}

// envBool returns true if the named environment variable is set to a
// truthy value (true, 1, yes).
func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "true" || v == "1" || v == "yes"
}

// envDefault returns the value of the named environment variable, or
// the default if the variable is empty.
func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
