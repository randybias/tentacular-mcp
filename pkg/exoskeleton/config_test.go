package exoskeleton

import (
	"testing"
)

func TestLoadFromEnv_FullConfig(t *testing.T) {
	t.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
	t.Setenv("TENTACULAR_EXOSKELETON_POSTGRES_ENABLED", "true")
	t.Setenv("TENTACULAR_EXOSKELETON_NATS_ENABLED", "true")
	t.Setenv("TENTACULAR_EXOSKELETON_RUSTFS_ENABLED", "false")
	t.Setenv("TENTACULAR_EXOSKELETON_CLEANUP_ON_UNDEPLOY", "true")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "postgres.tentacular-exoskeleton.svc")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_PORT", "5432")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_DATABASE", "tentacular")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_USER", "tentacular_admin")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "secret123")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_SSLMODE", "require")
	t.Setenv("TENTACULAR_NATS_URL", "nats://nats.tentacular-exoskeleton.svc:4222")
	t.Setenv("TENTACULAR_NATS_TOKEN", "nats-token-abc")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if !cfg.PostgresEnabled {
		t.Error("expected PostgresEnabled=true")
	}
	if !cfg.NATSEnabled {
		t.Error("expected NATSEnabled=true")
	}
	if cfg.RustFSEnabled {
		t.Error("expected RustFSEnabled=false")
	}
	if !cfg.CleanupOnUndeploy {
		t.Error("expected CleanupOnUndeploy=true")
	}
	if cfg.Postgres.Host != "postgres.tentacular-exoskeleton.svc" {
		t.Errorf("Postgres.Host = %q, want %q", cfg.Postgres.Host, "postgres.tentacular-exoskeleton.svc")
	}
	if cfg.Postgres.Port != "5432" {
		t.Errorf("Postgres.Port = %q, want %q", cfg.Postgres.Port, "5432")
	}
	if cfg.Postgres.Database != "tentacular" {
		t.Errorf("Postgres.Database = %q, want %q", cfg.Postgres.Database, "tentacular")
	}
	if cfg.Postgres.User != "tentacular_admin" {
		t.Errorf("Postgres.User = %q, want %q", cfg.Postgres.User, "tentacular_admin")
	}
	if cfg.Postgres.Password != "secret123" {
		t.Errorf("Postgres.Password = %q, want %q", cfg.Postgres.Password, "secret123")
	}
	if cfg.Postgres.SSLMode != "require" {
		t.Errorf("Postgres.SSLMode = %q, want %q", cfg.Postgres.SSLMode, "require")
	}
	if cfg.NATS.URL != "nats://nats.tentacular-exoskeleton.svc:4222" {
		t.Errorf("NATS.URL = %q, want %q", cfg.NATS.URL, "nats://nats.tentacular-exoskeleton.svc:4222")
	}
	if cfg.NATS.Token != "nats-token-abc" {
		t.Errorf("NATS.Token = %q, want %q", cfg.NATS.Token, "nats-token-abc")
	}
}

func TestLoadFromEnv_PartialConfig_OnlyPostgres(t *testing.T) {
	t.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "true")
	t.Setenv("TENTACULAR_EXOSKELETON_POSTGRES_ENABLED", "true")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_HOST", "pg.local")
	t.Setenv("TENTACULAR_POSTGRES_ADMIN_PASSWORD", "pw")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if !cfg.PostgresEnabled {
		t.Error("expected PostgresEnabled=true")
	}
	if cfg.NATSEnabled {
		t.Error("expected NATSEnabled=false (not set)")
	}
	// Check defaults are applied
	if cfg.Postgres.Port != "5432" {
		t.Errorf("Postgres.Port default = %q, want %q", cfg.Postgres.Port, "5432")
	}
	if cfg.Postgres.Database != "tentacular" {
		t.Errorf("Postgres.Database default = %q, want %q", cfg.Postgres.Database, "tentacular")
	}
	if cfg.Postgres.User != "tentacular_admin" {
		t.Errorf("Postgres.User default = %q, want %q", cfg.Postgres.User, "tentacular_admin")
	}
	if cfg.Postgres.SSLMode != "disable" {
		t.Errorf("Postgres.SSLMode default = %q, want %q", cfg.Postgres.SSLMode, "disable")
	}
}

func TestLoadFromEnv_Disabled(t *testing.T) {
	// No env vars set — everything defaults to false/empty
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}

	if cfg.Enabled {
		t.Error("expected Enabled=false when not set")
	}
	if cfg.PostgresEnabled {
		t.Error("expected PostgresEnabled=false")
	}
	if cfg.NATSEnabled {
		t.Error("expected NATSEnabled=false")
	}
}

func TestLoadFromEnv_CleanupDefaultsTrue(t *testing.T) {
	// CleanupOnUndeploy defaults to true when not set
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}

	if !cfg.CleanupOnUndeploy {
		t.Error("expected CleanupOnUndeploy to default to true")
	}
}

func TestLoadFromEnv_InvalidBoolDefaults(t *testing.T) {
	t.Setenv("TENTACULAR_EXOSKELETON_ENABLED", "notabool")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error: %v", err)
	}

	if cfg.Enabled {
		t.Error("expected Enabled=false for invalid bool")
	}
}

func TestValidate_Disabled(t *testing.T) {
	cfg := &ExoskeletonConfig{Enabled: false}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() disabled config should not error, got: %v", err)
	}
}

func TestValidate_PostgresEnabled_MissingHost(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		Postgres: PostgresConfig{
			Password: "secret",
			// Host is missing
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail when Postgres enabled but host missing")
	}
	if got := err.Error(); !contains(got, "TENTACULAR_POSTGRES_ADMIN_HOST") {
		t.Errorf("error should mention missing host env var, got: %s", got)
	}
}

func TestValidate_PostgresEnabled_MissingPassword(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		Postgres: PostgresConfig{
			Host: "pg.local",
			// Password is missing
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail when Postgres enabled but password missing")
	}
	if got := err.Error(); !contains(got, "TENTACULAR_POSTGRES_ADMIN_PASSWORD") {
		t.Errorf("error should mention missing password env var, got: %s", got)
	}
}

func TestValidate_NATSEnabled_MissingURL(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:     true,
		NATSEnabled: true,
		NATS:        NATSConfig{Token: "tok"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail when NATS enabled but URL missing")
	}
	if got := err.Error(); !contains(got, "TENTACULAR_NATS_URL") {
		t.Errorf("error should mention missing NATS URL env var, got: %s", got)
	}
}

func TestValidate_AllServicesValid(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		NATSEnabled:     true,
		Postgres: PostgresConfig{
			Host:     "pg.local",
			Password: "secret",
		},
		NATS: NATSConfig{
			URL: "nats://localhost:4222",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass for valid config, got: %v", err)
	}
}

func TestValidate_MultipleFieldsMissing(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		NATSEnabled:     true,
		// All connection fields missing
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail with multiple missing fields")
	}
	got := err.Error()
	if !contains(got, "TENTACULAR_POSTGRES_ADMIN_HOST") {
		t.Errorf("error should mention missing Postgres host, got: %s", got)
	}
	if !contains(got, "TENTACULAR_POSTGRES_ADMIN_PASSWORD") {
		t.Errorf("error should mention missing Postgres password, got: %s", got)
	}
	if !contains(got, "TENTACULAR_NATS_URL") {
		t.Errorf("error should mention missing NATS URL, got: %s", got)
	}
}

func TestValidate_RustFSEnabled_MissingFields(t *testing.T) {
	cfg := &ExoskeletonConfig{
		Enabled:       true,
		RustFSEnabled: true,
		// RustFS fields missing
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail when RustFS enabled but fields missing")
	}
	got := err.Error()
	if !contains(got, "TENTACULAR_RUSTFS_ENDPOINT") {
		t.Errorf("error should mention missing RustFS endpoint, got: %s", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
