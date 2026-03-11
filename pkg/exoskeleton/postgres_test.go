package exoskeleton

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// mockResult implements sql.Result for testing.
type mockResult struct{}

func (m mockResult) LastInsertId() (int64, error) { return 0, nil }
func (m mockResult) RowsAffected() (int64, error) { return 1, nil }

// mockDBQuerier records executed SQL and returns configurable results.
type mockDBQuerier struct {
	execCalls     []execCall
	queryCalls    []queryCall
	execErr       error
	queryResults  map[string][]any // query prefix -> scan values
	queryErr      error
	failOnQuery   string // if set, fail when query contains this string
}

type execCall struct {
	Query string
	Args  []any
}

type queryCall struct {
	Query string
	Args  []any
}

func newMockDB() *mockDBQuerier {
	return &mockDBQuerier{
		queryResults: make(map[string][]any),
	}
}

func (m *mockDBQuerier) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	m.execCalls = append(m.execCalls, execCall{Query: query, Args: args})
	if m.failOnQuery != "" && strings.Contains(query, m.failOnQuery) {
		return nil, fmt.Errorf("mock exec error on: %s", m.failOnQuery)
	}
	if m.execErr != nil {
		return nil, m.execErr
	}
	return mockResult{}, nil
}

func (m *mockDBQuerier) QueryRowContext(_ context.Context, query string, args ...any) *sql.Row {
	m.queryCalls = append(m.queryCalls, queryCall{Query: query, Args: args})
	return nil
}

func (m *mockDBQuerier) QueryRowScan(_ context.Context, query string, dest []any, args ...any) error {
	m.queryCalls = append(m.queryCalls, queryCall{Query: query, Args: args})
	if m.queryErr != nil {
		return m.queryErr
	}
	// Match query prefix to results
	for prefix, vals := range m.queryResults {
		if strings.Contains(query, prefix) {
			for i, v := range vals {
				if i < len(dest) {
					switch d := dest[i].(type) {
					case *bool:
						if bv, ok := v.(bool); ok {
							*d = bv
						}
					case *string:
						if sv, ok := v.(string); ok {
							*d = sv
						}
					}
				}
			}
			return nil
		}
	}
	return nil
}

func TestRegister(t *testing.T) {
	mock := newMockDB()
	reg := NewPostgresRegistrar(PostgresConfig{
		Host:     "pg.example.com",
		Port:     "5432",
		Database: "tentacular",
		User:     "admin",
		Password: "adminpw",
		SSLMode:  "disable",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify 4 SQL statements were executed
	if len(mock.execCalls) != 4 {
		t.Fatalf("expected 4 exec calls, got %d", len(mock.execCalls))
	}

	// Verify CREATE ROLE (DO block)
	if !strings.Contains(mock.execCalls[0].Query, "CREATE ROLE") {
		t.Errorf("first exec should be CREATE ROLE, got: %s", mock.execCalls[0].Query)
	}
	if !strings.Contains(mock.execCalls[0].Query, id.PostgresRole) {
		t.Errorf("CREATE ROLE should reference role %s, got: %s", id.PostgresRole, mock.execCalls[0].Query)
	}

	// Verify ALTER ROLE (set password)
	if !strings.Contains(mock.execCalls[1].Query, "ALTER ROLE") {
		t.Errorf("second exec should be ALTER ROLE, got: %s", mock.execCalls[1].Query)
	}

	// Verify CREATE SCHEMA
	if !strings.Contains(mock.execCalls[2].Query, "CREATE SCHEMA") {
		t.Errorf("third exec should be CREATE SCHEMA, got: %s", mock.execCalls[2].Query)
	}
	if !strings.Contains(mock.execCalls[2].Query, id.PostgresSchema) {
		t.Errorf("CREATE SCHEMA should reference schema %s", id.PostgresSchema)
	}

	// Verify GRANT
	if !strings.Contains(mock.execCalls[3].Query, "GRANT") {
		t.Errorf("fourth exec should be GRANT, got: %s", mock.execCalls[3].Query)
	}

	// Verify registration result
	if result.Role != id.PostgresRole {
		t.Errorf("role mismatch: got %s, want %s", result.Role, id.PostgresRole)
	}
	if result.Schema != id.PostgresSchema {
		t.Errorf("schema mismatch: got %s, want %s", result.Schema, id.PostgresSchema)
	}
	if result.Password == "" {
		t.Error("password should not be empty")
	}
	if len(result.Password) != 64 { // 32 bytes hex-encoded
		t.Errorf("password should be 64 hex chars, got %d", len(result.Password))
	}
	if result.Host != "pg.example.com" {
		t.Errorf("host mismatch: got %s", result.Host)
	}
	if result.Port != "5432" {
		t.Errorf("port mismatch: got %s", result.Port)
	}
	if result.Database != "tentacular" {
		t.Errorf("database mismatch: got %s", result.Database)
	}
}

func TestRegisterUniquePasswords(t *testing.T) {
	mock := newMockDB()
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")

	result1, err := reg.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	result2, err := reg.Register(context.Background(), id)
	if err != nil {
		t.Fatalf("second Register failed: %v", err)
	}

	if result1.Password == result2.Password {
		t.Error("consecutive Register calls should produce unique passwords")
	}
}

func TestRegisterNotConnected(t *testing.T) {
	reg := NewPostgresRegistrar(PostgresConfig{})
	_, err := reg.Register(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestRegisterExecError(t *testing.T) {
	mock := newMockDB()
	mock.failOnQuery = "CREATE ROLE"
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	_, err := reg.Register(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on CREATE ROLE failure")
	}
	if !strings.Contains(err.Error(), "create role") {
		t.Errorf("expected 'create role' error, got: %v", err)
	}
}

func TestReRegisterRoleExists(t *testing.T) {
	mock := newMockDB()
	// Role exists, schema exists
	mock.queryResults["pg_roles"] = []any{true}
	mock.queryResults["information_schema.schemata"] = []any{true}

	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.ReRegister(context.Background(), id)
	if err != nil {
		t.Fatalf("ReRegister failed: %v", err)
	}

	// Should NOT issue DROP statements
	for _, call := range mock.execCalls {
		if strings.Contains(call.Query, "DROP") {
			t.Errorf("ReRegister should not issue DROP, got: %s", call.Query)
		}
	}

	// Should issue ALTER ROLE (password rotation) and GRANT
	hasAlter := false
	hasGrant := false
	for _, call := range mock.execCalls {
		if strings.Contains(call.Query, "ALTER ROLE") {
			hasAlter = true
		}
		if strings.Contains(call.Query, "GRANT") {
			hasGrant = true
		}
	}
	if !hasAlter {
		t.Error("ReRegister should rotate password via ALTER ROLE")
	}
	if !hasGrant {
		t.Error("ReRegister should ensure grants")
	}

	if result.Password == "" {
		t.Error("password should not be empty")
	}
}

func TestReRegisterRoleMissing(t *testing.T) {
	mock := newMockDB()
	// Role does NOT exist, schema exists
	mock.queryResults["pg_roles"] = []any{false}
	mock.queryResults["information_schema.schemata"] = []any{true}

	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	result, err := reg.ReRegister(context.Background(), id)
	if err != nil {
		t.Fatalf("ReRegister failed: %v", err)
	}

	// Should create the missing role
	hasCreate := false
	for _, call := range mock.execCalls {
		if strings.Contains(call.Query, "CREATE ROLE") {
			hasCreate = true
		}
	}
	if !hasCreate {
		t.Error("ReRegister should CREATE ROLE when role is missing (drift repair)")
	}

	if result.Role != id.PostgresRole {
		t.Errorf("role mismatch: got %s, want %s", result.Role, id.PostgresRole)
	}
}

func TestReRegisterSchemaMissing(t *testing.T) {
	mock := newMockDB()
	// Role exists, schema does NOT exist
	mock.queryResults["pg_roles"] = []any{true}
	mock.queryResults["information_schema.schemata"] = []any{false}

	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	_, err := reg.ReRegister(context.Background(), id)
	if err != nil {
		t.Fatalf("ReRegister failed: %v", err)
	}

	// Should create the missing schema
	hasCreateSchema := false
	for _, call := range mock.execCalls {
		if strings.Contains(call.Query, "CREATE SCHEMA") {
			hasCreateSchema = true
		}
	}
	if !hasCreateSchema {
		t.Error("ReRegister should CREATE SCHEMA when schema is missing (drift repair)")
	}
}

func TestReRegisterNotConnected(t *testing.T) {
	reg := NewPostgresRegistrar(PostgresConfig{})
	_, err := reg.ReRegister(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestReRegisterQueryError(t *testing.T) {
	mock := newMockDB()
	mock.queryErr = fmt.Errorf("connection refused")

	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	_, err := reg.ReRegister(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on query failure")
	}
	if !strings.Contains(err.Error(), "check role") {
		t.Errorf("expected 'check role' error, got: %v", err)
	}
}

func TestUnregister(t *testing.T) {
	mock := newMockDB()
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	err := reg.Unregister(context.Background(), id)
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	// Verify DROP SCHEMA CASCADE and DROP ROLE
	if len(mock.execCalls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(mock.execCalls))
	}

	if !strings.Contains(mock.execCalls[0].Query, "DROP SCHEMA") {
		t.Errorf("first exec should be DROP SCHEMA, got: %s", mock.execCalls[0].Query)
	}
	if !strings.Contains(mock.execCalls[0].Query, "CASCADE") {
		t.Errorf("DROP SCHEMA should include CASCADE, got: %s", mock.execCalls[0].Query)
	}
	if !strings.Contains(mock.execCalls[0].Query, id.PostgresSchema) {
		t.Errorf("DROP SCHEMA should reference schema %s", id.PostgresSchema)
	}

	if !strings.Contains(mock.execCalls[1].Query, "DROP ROLE") {
		t.Errorf("second exec should be DROP ROLE, got: %s", mock.execCalls[1].Query)
	}
	if !strings.Contains(mock.execCalls[1].Query, id.PostgresRole) {
		t.Errorf("DROP ROLE should reference role %s", id.PostgresRole)
	}
}

func TestUnregisterNotConnected(t *testing.T) {
	reg := NewPostgresRegistrar(PostgresConfig{})
	err := reg.Unregister(context.Background(), CompileIdentity("ns", "wf"))
	if err == nil {
		t.Fatal("expected error for unconnected registrar")
	}
}

func TestUnregisterDropRoleError(t *testing.T) {
	mock := newMockDB()
	mock.failOnQuery = "DROP ROLE"
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	err := reg.Unregister(context.Background(), id)
	if err == nil {
		t.Fatal("expected error on DROP ROLE failure")
	}
	if !strings.Contains(err.Error(), "drop role") {
		t.Errorf("expected 'drop role' error, got: %v", err)
	}
}

func TestUnregisterDropSchemaError(t *testing.T) {
	mock := newMockDB()
	mock.failOnQuery = "DROP SCHEMA"
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	id := CompileIdentity("tent-myapp", "hello-world")
	err := reg.Unregister(context.Background(), id)
	if err == nil {
		t.Fatal("expected error on DROP SCHEMA failure")
	}
	if !strings.Contains(err.Error(), "drop schema") {
		t.Errorf("expected 'drop schema' error, got: %v", err)
	}
}

func TestRegisterGrantError(t *testing.T) {
	mock := newMockDB()
	mock.failOnQuery = "GRANT"
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	_, err := reg.Register(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on GRANT failure")
	}
	if !strings.Contains(err.Error(), "grant") {
		t.Errorf("expected 'grant' error, got: %v", err)
	}
}

func TestRegisterSchemaError(t *testing.T) {
	mock := newMockDB()
	mock.failOnQuery = "CREATE SCHEMA"
	reg := NewPostgresRegistrar(PostgresConfig{
		Host: "pg.example.com", Port: "5432", Database: "tentacular",
	})
	reg.SetDB(mock)

	_, err := reg.Register(context.Background(), CompileIdentity("tent-myapp", "wf"))
	if err == nil {
		t.Fatal("expected error on CREATE SCHEMA failure")
	}
	if !strings.Contains(err.Error(), "create schema") {
		t.Errorf("expected 'create schema' error, got: %v", err)
	}
}

func TestPasswordGeneration(t *testing.T) {
	passwords := make(map[string]bool)
	for i := 0; i < 100; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword failed: %v", err)
		}
		if len(pw) != 64 {
			t.Errorf("password should be 64 hex chars, got %d", len(pw))
		}
		if passwords[pw] {
			t.Errorf("duplicate password generated: %s", pw)
		}
		passwords[pw] = true
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"tn_myapp_wf", `"tn_myapp_wf"`},
		{"simple", `"simple"`},
	}
	for _, tt := range tests {
		got := quoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
