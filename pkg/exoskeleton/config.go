package exoskeleton

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ExoskeletonConfig holds the complete exoskeleton subsystem configuration,
// parsed from environment variables at startup.
type ExoskeletonConfig struct {
	Enabled          bool
	PostgresEnabled  bool
	NATSEnabled      bool
	RustFSEnabled    bool // reserved for future use
	CleanupOnUndeploy bool

	Postgres PostgresConfig
	NATS     NATSConfig
	RustFS   RustFSConfig // reserved for future use
}

// PostgresConfig holds connection details for the admin Postgres connection.
type PostgresConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	SSLMode  string
}

// NATSConfig holds connection details for the NATS server.
type NATSConfig struct {
	URL   string
	Token string
}

// RustFSConfig holds connection details for RustFS (future use).
type RustFSConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
}

// LoadFromEnv reads exoskeleton configuration from environment variables.
// All variables are prefixed with TENTACULAR_.
func LoadFromEnv() (*ExoskeletonConfig, error) {
	cfg := &ExoskeletonConfig{
		Enabled:          envBool("TENTACULAR_EXOSKELETON_ENABLED", false),
		PostgresEnabled:  envBool("TENTACULAR_EXOSKELETON_POSTGRES_ENABLED", false),
		NATSEnabled:      envBool("TENTACULAR_EXOSKELETON_NATS_ENABLED", false),
		RustFSEnabled:    envBool("TENTACULAR_EXOSKELETON_RUSTFS_ENABLED", false),
		CleanupOnUndeploy: envBool("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY", true),

		Postgres: PostgresConfig{
			Host:     os.Getenv("TENTACULAR_POSTGRES_ADMIN_HOST"),
			Port:     envDefault("TENTACULAR_POSTGRES_ADMIN_PORT", "5432"),
			Database: envDefault("TENTACULAR_POSTGRES_ADMIN_DATABASE", "tentacular"),
			User:     envDefault("TENTACULAR_POSTGRES_ADMIN_USER", "tentacular_admin"),
			Password: os.Getenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD"),
			SSLMode:  envDefault("TENTACULAR_POSTGRES_ADMIN_SSLMODE", "disable"),
		},

		NATS: NATSConfig{
			URL:   os.Getenv("TENTACULAR_NATS_URL"),
			Token: os.Getenv("TENTACULAR_NATS_TOKEN"),
		},

		RustFS: RustFSConfig{
			Endpoint:  os.Getenv("TENTACULAR_RUSTFS_ENDPOINT"),
			AccessKey: os.Getenv("TENTACULAR_RUSTFS_ACCESS_KEY"),
			SecretKey: os.Getenv("TENTACULAR_RUSTFS_SECRET_KEY"),
			Bucket:    os.Getenv("TENTACULAR_RUSTFS_BUCKET"),
			Region:    os.Getenv("TENTACULAR_RUSTFS_REGION"),
		},
	}

	return cfg, nil
}

// Validate checks that all required connection fields are set for each
// enabled service. Returns an error describing all missing fields.
func (c *ExoskeletonConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	var missing []string

	if c.PostgresEnabled {
		if c.Postgres.Host == "" {
			missing = append(missing, "TENTACULAR_POSTGRES_ADMIN_HOST")
		}
		if c.Postgres.Password == "" {
			missing = append(missing, "TENTACULAR_POSTGRES_ADMIN_PASSWORD")
		}
	}

	if c.NATSEnabled {
		if c.NATS.URL == "" {
			missing = append(missing, "TENTACULAR_NATS_URL")
		}
		if c.NATS.Token == "" {
			missing = append(missing, "TENTACULAR_NATS_TOKEN")
		}
	}

	if c.RustFSEnabled {
		if c.RustFS.Endpoint == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_ENDPOINT")
		}
		if c.RustFS.AccessKey == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_ACCESS_KEY")
		}
		if c.RustFS.SecretKey == "" {
			missing = append(missing, "TENTACULAR_RUSTFS_SECRET_KEY")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("exoskeleton: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

func envDefault(key, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}
