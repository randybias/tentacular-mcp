package exoskeleton

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCreds holds the connection details returned after registering
// a tentacle with Postgres.
type PostgresCreds struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	Schema   string `json:"schema"`
	Protocol string `json:"protocol"`
}

// PostgresRegistrar manages per-tentacle Postgres roles and schemas.
type PostgresRegistrar struct {
	pool *pgxpool.Pool
	cfg  PostgresConfig
}

// NewPostgresRegistrar creates a new registrar with an admin connection pool.
func NewPostgresRegistrar(ctx context.Context, cfg PostgresConfig) (*PostgresRegistrar, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(cfg.User), url.QueryEscape(cfg.Password), cfg.Host, cfg.Port, cfg.Database, sslMode)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("postgres admin connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres admin ping: %w", err)
	}
	return &PostgresRegistrar{pool: pool, cfg: cfg}, nil
}

// Register creates (or updates) a scoped Postgres role and schema for
// the given identity. It is idempotent: re-registration rotates the
// password but preserves the schema and its data.
func (r *PostgresRegistrar) Register(ctx context.Context, id Identity) (*PostgresCreds, error) {
	password, err := generateHexPassword(32)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}

	role := id.PgRole
	schema := id.PgSchema

	// Use a single transaction for all DDL.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// CREATE ROLE IF NOT EXISTS -- Postgres doesn't have IF NOT EXISTS for
	// CREATE ROLE before v16, so we use a DO block for compatibility.
	// The admin user must have CREATEROLE privilege (not SUPERUSER).
	createRole := fmt.Sprintf(
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '%s') THEN
				CREATE ROLE %s LOGIN PASSWORD '%s';
			ELSE
				ALTER ROLE %s WITH PASSWORD '%s';
			END IF;
		END $$`,
		escapeLiteral(role), pgIdent(role), escapeLiteral(password),
		pgIdent(role), escapeLiteral(password),
	)
	if _, err := tx.Exec(ctx, createRole); err != nil {
		return nil, fmt.Errorf("create/alter role %s: %w", role, err)
	}

	// GRANT the new role to the admin user so that CREATE SCHEMA ... AUTHORIZATION
	// works without SUPERUSER. This is a no-op if the grant already exists.
	grantToAdmin := fmt.Sprintf("GRANT %s TO CURRENT_USER", pgIdent(role))
	if _, err := tx.Exec(ctx, grantToAdmin); err != nil {
		return nil, fmt.Errorf("grant role %s to admin: %w", role, err)
	}

	// CREATE SCHEMA IF NOT EXISTS
	createSchema := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s",
		pgIdent(schema), pgIdent(role))
	if _, err := tx.Exec(ctx, createSchema); err != nil {
		return nil, fmt.Errorf("create schema %s: %w", schema, err)
	}

	// GRANT USAGE + ALL on the schema
	grantUsage := fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", pgIdent(schema), pgIdent(role))
	if _, err := tx.Exec(ctx, grantUsage); err != nil {
		return nil, fmt.Errorf("grant usage on schema %s: %w", schema, err)
	}
	grantAll := fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s TO %s",
		pgIdent(schema), pgIdent(role))
	if _, err := tx.Exec(ctx, grantAll); err != nil {
		return nil, fmt.Errorf("grant all on tables in %s: %w", schema, err)
	}

	// ALTER DEFAULT PRIVILEGES so future tables are also accessible.
	alterDefault := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT ALL PRIVILEGES ON TABLES TO %s",
		pgIdent(schema), pgIdent(role))
	if _, err := tx.Exec(ctx, alterDefault); err != nil {
		return nil, fmt.Errorf("alter default privileges in %s: %w", schema, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	slog.Info("postgres: registered tentacle", "role", role, "schema", schema)

	return &PostgresCreds{
		Host:     r.cfg.Host,
		Port:     r.cfg.Port,
		Database: r.cfg.Database,
		User:     role,
		Password: password,
		Schema:   schema,
		Protocol: "postgresql",
	}, nil
}

// Unregister drops the role and schema (CASCADE) for the given identity.
func (r *PostgresRegistrar) Unregister(ctx context.Context, id Identity) error {
	role := id.PgRole
	schema := id.PgSchema

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Revoke all privileges first.
	revoke := fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s FROM %s",
		pgIdent(schema), pgIdent(role))
	if _, err := tx.Exec(ctx, revoke); err != nil {
		slog.Warn("postgres: revoke failed (may not exist)", "role", role, "error", err)
	}

	dropSchema := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(schema))
	if _, err := tx.Exec(ctx, dropSchema); err != nil {
		return fmt.Errorf("drop schema %s: %w", schema, err)
	}

	dropRole := "DROP ROLE IF EXISTS " + pgIdent(role)
	if _, err := tx.Exec(ctx, dropRole); err != nil {
		return fmt.Errorf("drop role %s: %w", role, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	slog.Info("postgres: unregistered tentacle", "role", role, "schema", schema)
	return nil
}

// Close closes the admin connection pool.
func (r *PostgresRegistrar) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

// generateHexPassword generates a random hex-encoded password of the
// specified number of hex characters (each byte = 2 hex chars).
func generateHexPassword(hexChars int) (string, error) {
	b := make([]byte, hexChars/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// pgIdent double-quotes a Postgres identifier to prevent SQL injection.
// Embedded double quotes are escaped by doubling them per SQL standard.
func pgIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// escapeLiteral escapes single quotes and backslashes in a Postgres string literal.
func escapeLiteral(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '\'':
			b.WriteString("''")
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// EnsureEnclave provisions the enclave-level Postgres database if it does not
// already exist. Phase 0 stub: no-op placeholder. Full implementation in Phase 1.
func (*PostgresRegistrar) EnsureEnclave(_ context.Context, id EnclaveIdentity) error {
	slog.Info("exoskeleton: postgres EnsureEnclave (stub)", "enclave", id.Enclave, "db", id.PgDB)
	return nil
}

// CleanupEnclave drops the enclave-level Postgres database.
// Phase 0 stub: no-op placeholder. Full implementation in Phase 1.
func (*PostgresRegistrar) CleanupEnclave(_ context.Context, id EnclaveIdentity) error {
	slog.Info("exoskeleton: postgres CleanupEnclave (stub)", "enclave", id.Enclave, "db", id.PgDB)
	return nil
}
