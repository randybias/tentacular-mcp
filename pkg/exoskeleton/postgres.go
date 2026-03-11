package exoskeleton

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// DBExecutor abstracts database operations to allow unit testing with mocks.
type DBExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DBRow abstracts a single row result for testability.
type DBRow interface {
	Scan(dest ...any) error
}

// DBQuerier extends DBExecutor with a queryable row interface for testing.
type DBQuerier interface {
	DBExecutor
	QueryRowScan(ctx context.Context, query string, dest []any, args ...any) error
}

// PostgresRegistration holds the result of a successful registration.
type PostgresRegistration struct {
	Role     string
	Schema   string
	Password string
	Host     string
	Port     string
	Database string
}

// PostgresRegistrar provisions per-tentacle Postgres roles and schemas.
type PostgresRegistrar struct {
	config  PostgresConfig
	db      DBQuerier
}

// validIdentifier matches safe Postgres identifiers (alphanumeric + underscore).
var validIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// NewPostgresRegistrar creates a registrar with the given config.
func NewPostgresRegistrar(config PostgresConfig) *PostgresRegistrar {
	return &PostgresRegistrar{
		config: config,
	}
}

// SetDB sets the database executor for the registrar. This is used to inject
// a mock for testing.
func (r *PostgresRegistrar) SetDB(db DBQuerier) {
	r.db = db
}

// Connect opens a real database connection using the registrar's config.
// Must be called before Register/Unregister can be used in production.
func (r *PostgresRegistrar) Connect() error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(r.config.User),
		url.QueryEscape(r.config.Password),
		r.config.Host, r.config.Port,
		url.QueryEscape(r.config.Database),
		url.QueryEscape(r.config.SSLMode))
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("postgres registrar: open connection: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return fmt.Errorf("postgres registrar: ping: %w", err)
	}
	r.db = &sqlDBAdapter{db: db}
	return nil
}

// Close closes the underlying database connection pool.
func (r *PostgresRegistrar) Close() error {
	if a, ok := r.db.(*sqlDBAdapter); ok && a.db != nil {
		return a.db.Close()
	}
	return nil
}

// sqlDBAdapter wraps *sql.DB to implement the DBQuerier interface.
type sqlDBAdapter struct {
	db *sql.DB
}

func (a *sqlDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.db.ExecContext(ctx, query, args...)
}

func (a *sqlDBAdapter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return a.db.QueryRowContext(ctx, query, args...)
}

func (a *sqlDBAdapter) QueryRowScan(ctx context.Context, query string, dest []any, args ...any) error {
	return a.db.QueryRowContext(ctx, query, args...).Scan(dest...)
}

// Register provisions a new Postgres role and schema for the given identity.
// It generates a strong random password and executes idempotent SQL.
func (r *PostgresRegistrar) Register(ctx context.Context, id Identity) (*PostgresRegistration, error) {
	if r.db == nil {
		return nil, fmt.Errorf("postgres registrar: not connected")
	}

	if !validIdentifier.MatchString(id.PostgresRole) {
		return nil, fmt.Errorf("postgres registrar: invalid role name %q", id.PostgresRole)
	}
	if !validIdentifier.MatchString(id.PostgresSchema) {
		return nil, fmt.Errorf("postgres registrar: invalid schema name %q", id.PostgresSchema)
	}

	password, err := generateRandomHex()
	if err != nil {
		return nil, fmt.Errorf("postgres registrar: failed to generate password: %w", err)
	}

	// CREATE ROLE IF NOT EXISTS is not standard SQL; use a DO block for idempotency.
	// Use quoted identifier for the role name in the password setting.
	createRoleSQL := fmt.Sprintf(
		`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s WITH LOGIN PASSWORD '%s'; END IF; END $$`,
		id.PostgresRole, quoteIdent(id.PostgresRole), password,
	)
	if _, err := r.db.ExecContext(ctx, createRoleSQL); err != nil {
		return nil, fmt.Errorf("postgres registrar: create role: %w", err)
	}

	// Set password (handles case where role already existed with different password)
	setPasswordSQL := fmt.Sprintf(
		`ALTER ROLE %s WITH LOGIN PASSWORD '%s'`,
		quoteIdent(id.PostgresRole), password,
	)
	if _, err := r.db.ExecContext(ctx, setPasswordSQL); err != nil {
		return nil, fmt.Errorf("postgres registrar: set password: %w", err)
	}

	createSchemaSQL := fmt.Sprintf(
		`CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s`,
		quoteIdent(id.PostgresSchema), quoteIdent(id.PostgresRole),
	)
	if _, err := r.db.ExecContext(ctx, createSchemaSQL); err != nil {
		return nil, fmt.Errorf("postgres registrar: create schema: %w", err)
	}

	grantSQL := fmt.Sprintf(
		`GRANT USAGE, CREATE ON SCHEMA %s TO %s`,
		quoteIdent(id.PostgresSchema), quoteIdent(id.PostgresRole),
	)
	if _, err := r.db.ExecContext(ctx, grantSQL); err != nil {
		return nil, fmt.Errorf("postgres registrar: grant: %w", err)
	}

	return &PostgresRegistration{
		Role:     id.PostgresRole,
		Schema:   id.PostgresSchema,
		Password: password,
		Host:     r.config.Host,
		Port:     r.config.Port,
		Database: r.config.Database,
	}, nil
}

// ReRegister verifies an existing registration and repairs drift without
// destructive operations. It does NOT drop or recreate the schema.
func (r *PostgresRegistrar) ReRegister(ctx context.Context, id Identity) (*PostgresRegistration, error) {
	if r.db == nil {
		return nil, fmt.Errorf("postgres registrar: not connected")
	}

	if !validIdentifier.MatchString(id.PostgresRole) {
		return nil, fmt.Errorf("postgres registrar: invalid role name %q", id.PostgresRole)
	}
	if !validIdentifier.MatchString(id.PostgresSchema) {
		return nil, fmt.Errorf("postgres registrar: invalid schema name %q", id.PostgresSchema)
	}

	// Check if role exists
	var roleExists bool
	err := r.db.QueryRowScan(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)`,
		[]any{&roleExists},
		id.PostgresRole,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres registrar: check role: %w", err)
	}

	// If role doesn't exist, create it (handles drift)
	password, err := generateRandomHex()
	if err != nil {
		return nil, fmt.Errorf("postgres registrar: failed to generate password: %w", err)
	}

	if !roleExists {
		createRoleSQL := fmt.Sprintf(
			`CREATE ROLE %s WITH LOGIN PASSWORD '%s'`,
			quoteIdent(id.PostgresRole), password,
		)
		if _, err := r.db.ExecContext(ctx, createRoleSQL); err != nil {
			return nil, fmt.Errorf("postgres registrar: create role (drift repair): %w", err)
		}
	} else {
		// Rotate password on re-register
		setPasswordSQL := fmt.Sprintf(
			`ALTER ROLE %s WITH PASSWORD '%s'`,
			quoteIdent(id.PostgresRole), password,
		)
		if _, err := r.db.ExecContext(ctx, setPasswordSQL); err != nil {
			return nil, fmt.Errorf("postgres registrar: rotate password: %w", err)
		}
	}

	// Check if schema exists
	var schemaExists bool
	err = r.db.QueryRowScan(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`,
		[]any{&schemaExists},
		id.PostgresSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres registrar: check schema: %w", err)
	}

	if !schemaExists {
		createSchemaSQL := fmt.Sprintf(
			`CREATE SCHEMA %s AUTHORIZATION %s`,
			quoteIdent(id.PostgresSchema), quoteIdent(id.PostgresRole),
		)
		if _, err := r.db.ExecContext(ctx, createSchemaSQL); err != nil {
			return nil, fmt.Errorf("postgres registrar: create schema (drift repair): %w", err)
		}
	}

	// Ensure grants are correct
	grantSQL := fmt.Sprintf(
		`GRANT USAGE, CREATE ON SCHEMA %s TO %s`,
		quoteIdent(id.PostgresSchema), quoteIdent(id.PostgresRole),
	)
	if _, err := r.db.ExecContext(ctx, grantSQL); err != nil {
		return nil, fmt.Errorf("postgres registrar: grant: %w", err)
	}

	return &PostgresRegistration{
		Role:     id.PostgresRole,
		Schema:   id.PostgresSchema,
		Password: password,
		Host:     r.config.Host,
		Port:     r.config.Port,
		Database: r.config.Database,
	}, nil
}

// Unregister drops the schema (CASCADE) and role for the given identity.
func (r *PostgresRegistrar) Unregister(ctx context.Context, id Identity) error {
	if r.db == nil {
		return fmt.Errorf("postgres registrar: not connected")
	}

	if !validIdentifier.MatchString(id.PostgresRole) {
		return fmt.Errorf("postgres registrar: invalid role name %q", id.PostgresRole)
	}
	if !validIdentifier.MatchString(id.PostgresSchema) {
		return fmt.Errorf("postgres registrar: invalid schema name %q", id.PostgresSchema)
	}

	dropSchemaSQL := fmt.Sprintf(
		`DROP SCHEMA IF EXISTS %s CASCADE`,
		quoteIdent(id.PostgresSchema),
	)
	if _, err := r.db.ExecContext(ctx, dropSchemaSQL); err != nil {
		return fmt.Errorf("postgres registrar: drop schema: %w", err)
	}

	dropRoleSQL := fmt.Sprintf(
		`DROP ROLE IF EXISTS %s`,
		quoteIdent(id.PostgresRole),
	)
	if _, err := r.db.ExecContext(ctx, dropRoleSQL); err != nil {
		// Log warning but don't fail — role may have dependent objects in other schemas
		return fmt.Errorf("postgres registrar: drop role (may have dependents): %w", err)
	}

	return nil
}

// generateRandomHex creates a cryptographically random 32-byte hex-encoded string.
// Used for both Postgres passwords and NATS tokens.
func generateRandomHex() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// quoteIdent quotes a Postgres identifier with double quotes.
// Embedded double quotes are escaped by doubling them per SQL standard.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
