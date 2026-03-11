package exoskeleton

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockPGRegistrar implements PostgresRegistrarI for testing.
type mockPGRegistrar struct {
	registerCalls   []Identity
	unregisterCalls []Identity
	registerErr     error
	unregisterErr   error
	registration    *PostgresRegistration
}

func newMockPGRegistrar() *mockPGRegistrar {
	return &mockPGRegistrar{
		registration: &PostgresRegistration{
			Role:     "tn_test_role",
			Schema:   "tn_test_schema",
			Password: "test-password",
			Host:     "pg.test",
			Port:     "5432",
			Database: "testdb",
		},
	}
}

func (m *mockPGRegistrar) Register(_ context.Context, id Identity) (*PostgresRegistration, error) {
	m.registerCalls = append(m.registerCalls, id)
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.registration, nil
}

func (m *mockPGRegistrar) Unregister(_ context.Context, id Identity) error {
	m.unregisterCalls = append(m.unregisterCalls, id)
	return m.unregisterErr
}

// mockNATSReg implements NATSRegistrarI for testing.
type mockNATSReg struct {
	registerCalls   []Identity
	unregisterCalls []Identity
	registerErr     error
	unregisterErr   error
	registration    *NATSRegistration
}

func newMockNATSReg() *mockNATSReg {
	return &mockNATSReg{
		registration: &NATSRegistration{
			URL:           "nats://test:4222",
			Token:         "test-nats-token",
			SubjectPrefix: "tentacular.test.wf",
			Principal:     "test.wf",
		},
	}
}

func (m *mockNATSReg) Register(_ context.Context, id Identity) (*NATSRegistration, error) {
	m.registerCalls = append(m.registerCalls, id)
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.registration, nil
}

func (m *mockNATSReg) Unregister(_ context.Context, id Identity) error {
	m.unregisterCalls = append(m.unregisterCalls, id)
	return m.unregisterErr
}

// mockCredInjector implements CredentialInjectorI for testing.
type mockCredInjector struct {
	injectCalls []mockInjectCall
	removeCalls []mockRemoveCall
	injectErr   error
	removeErr   error
}

type mockInjectCall struct {
	Namespace string
	Workflow  string
	PGReg     *PostgresRegistration
	NATSReg   *NATSRegistration
}

type mockRemoveCall struct {
	Namespace string
	Workflow  string
}

func (m *mockCredInjector) Inject(_ context.Context, namespace, workflow string, pgReg *PostgresRegistration, natsReg *NATSRegistration) error {
	m.injectCalls = append(m.injectCalls, mockInjectCall{
		Namespace: namespace,
		Workflow:  workflow,
		PGReg:     pgReg,
		NATSReg:   natsReg,
	})
	return m.injectErr
}

func (m *mockCredInjector) Remove(_ context.Context, namespace, workflow string) error {
	m.removeCalls = append(m.removeCalls, mockRemoveCall{
		Namespace: namespace,
		Workflow:  workflow,
	})
	return m.removeErr
}

func testConfig() *ExoskeletonConfig {
	return &ExoskeletonConfig{
		Enabled:           true,
		PostgresEnabled:   true,
		NATSEnabled:       true,
		CleanupOnUndeploy: true,
		Postgres: PostgresConfig{
			Host:     "pg.test",
			Port:     "5432",
			Database: "testdb",
			User:     "admin",
			Password: "admin-pw",
		},
		NATS: NATSConfig{
			URL:   "nats://test:4222",
			Token: "admin-token",
		},
	}
}

func TestControllerRegisterBothServices(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	deps := []string{"tentacular-postgres", "tentacular-nats"}
	err := ctrl.Register(context.Background(), "tent-myapp", "hello-world", deps)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Both registrars should be called
	if len(pgMock.registerCalls) != 1 {
		t.Errorf("expected 1 postgres register call, got %d", len(pgMock.registerCalls))
	}
	if len(natsMock.registerCalls) != 1 {
		t.Errorf("expected 1 nats register call, got %d", len(natsMock.registerCalls))
	}

	// Credential injector should be called with both registrations
	if len(credMock.injectCalls) != 1 {
		t.Fatalf("expected 1 inject call, got %d", len(credMock.injectCalls))
	}
	call := credMock.injectCalls[0]
	if call.Namespace != "tent-myapp" {
		t.Errorf("namespace: got %s, want tent-myapp", call.Namespace)
	}
	if call.Workflow != "hello-world" {
		t.Errorf("workflow: got %s, want hello-world", call.Workflow)
	}
	if call.PGReg == nil {
		t.Error("postgres registration should not be nil")
	}
	if call.NATSReg == nil {
		t.Error("nats registration should not be nil")
	}
}

func TestControllerRegisterPostgresOnly(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	deps := []string{"tentacular-postgres"}
	err := ctrl.Register(context.Background(), "tent-myapp", "wf", deps)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Only postgres registrar should be called
	if len(pgMock.registerCalls) != 1 {
		t.Errorf("expected 1 postgres register call, got %d", len(pgMock.registerCalls))
	}
	if len(natsMock.registerCalls) != 0 {
		t.Errorf("NATS register should not be called, got %d calls", len(natsMock.registerCalls))
	}

	// Credential injector should be called with only PG
	if len(credMock.injectCalls) != 1 {
		t.Fatalf("expected 1 inject call, got %d", len(credMock.injectCalls))
	}
	if credMock.injectCalls[0].PGReg == nil {
		t.Error("postgres registration should not be nil")
	}
	if credMock.injectCalls[0].NATSReg != nil {
		t.Error("nats registration should be nil for postgres-only")
	}
}

func TestControllerRegisterDisabledServiceDep(t *testing.T) {
	cfg := testConfig()
	cfg.NATSEnabled = false
	pgMock := newMockPGRegistrar()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, nil, credMock)

	deps := []string{"tentacular-postgres", "tentacular-nats"}
	err := ctrl.Register(context.Background(), "tent-myapp", "wf", deps)
	if err == nil {
		t.Fatal("expected error when workflow depends on disabled service")
	}
	if !strings.Contains(err.Error(), "nats is not enabled") {
		t.Errorf("expected 'nats is not enabled' error, got: %v", err)
	}
}

func TestControllerRegisterPostgresError(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	pgMock.registerErr = fmt.Errorf("connection refused")
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	deps := []string{"tentacular-postgres"}
	err := ctrl.Register(context.Background(), "tent-myapp", "wf", deps)
	if err == nil {
		t.Fatal("expected error on postgres registration failure")
	}
	if !strings.Contains(err.Error(), "postgres registration") {
		t.Errorf("expected 'postgres registration' error, got: %v", err)
	}
	// Credential injector should NOT be called on failure
	if len(credMock.injectCalls) != 0 {
		t.Error("inject should not be called when registration fails")
	}
}

func TestControllerRegisterNoDeps(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	deps := []string{"redis", "external-api"}
	err := ctrl.Register(context.Background(), "tent-myapp", "wf", deps)
	if err != nil {
		t.Fatalf("Register with no tentacular deps should succeed: %v", err)
	}

	// No registrars should be called
	if len(pgMock.registerCalls) != 0 {
		t.Error("postgres should not be called")
	}
	if len(natsMock.registerCalls) != 0 {
		t.Error("nats should not be called")
	}
	// No credential injection when no deps
	if len(credMock.injectCalls) != 0 {
		t.Error("inject should not be called with no tentacular deps")
	}
}

func TestControllerRegisterDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.Enabled = false
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	deps := []string{"tentacular-postgres"}
	err := ctrl.Register(context.Background(), "tent-myapp", "wf", deps)
	if err != nil {
		t.Fatalf("Register with disabled exoskeleton should be a no-op: %v", err)
	}
	if len(pgMock.registerCalls) != 0 {
		t.Error("nothing should be called when exoskeleton is disabled")
	}
}

func TestControllerRegisterNilConfig(t *testing.T) {
	ctrl := NewExoskeletonController(nil, nil, nil, nil)
	err := ctrl.Register(context.Background(), "ns", "wf", []string{"tentacular-postgres"})
	if err != nil {
		t.Fatalf("Register with nil config should be a no-op: %v", err)
	}
}

func TestControllerUnregister(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	err := ctrl.Unregister(context.Background(), "tent-myapp", "hello-world")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	// Credential remove should be called
	if len(credMock.removeCalls) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(credMock.removeCalls))
	}
	if credMock.removeCalls[0].Namespace != "tent-myapp" {
		t.Errorf("namespace: got %s, want tent-myapp", credMock.removeCalls[0].Namespace)
	}
	if credMock.removeCalls[0].Workflow != "hello-world" {
		t.Errorf("workflow: got %s, want hello-world", credMock.removeCalls[0].Workflow)
	}

	// Both service unregistrations should be called
	if len(pgMock.unregisterCalls) != 1 {
		t.Errorf("expected 1 postgres unregister, got %d", len(pgMock.unregisterCalls))
	}
	if len(natsMock.unregisterCalls) != 1 {
		t.Errorf("expected 1 nats unregister, got %d", len(natsMock.unregisterCalls))
	}
}

func TestControllerUnregisterPartialFailure(t *testing.T) {
	cfg := testConfig()
	pgMock := newMockPGRegistrar()
	natsMock := newMockNATSReg()
	natsMock.unregisterErr = fmt.Errorf("nats unreachable")
	credMock := &mockCredInjector{}

	ctrl := NewExoskeletonController(cfg, pgMock, natsMock, credMock)

	err := ctrl.Unregister(context.Background(), "tent-myapp", "wf")
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !strings.Contains(err.Error(), "partial failures") {
		t.Errorf("expected 'partial failures' message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "nats") {
		t.Errorf("error should mention nats failure: %v", err)
	}

	// Credential remove and postgres unregister should still be called
	if len(credMock.removeCalls) != 1 {
		t.Error("credential remove should still be called on partial failure")
	}
	if len(pgMock.unregisterCalls) != 1 {
		t.Error("postgres unregister should still be called on partial failure")
	}
}

func TestControllerUnregisterDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.Enabled = false
	ctrl := NewExoskeletonController(cfg, nil, nil, nil)

	err := ctrl.Unregister(context.Background(), "tent-myapp", "wf")
	if err != nil {
		t.Fatalf("Unregister with disabled exoskeleton should be a no-op: %v", err)
	}
}

func TestDetectExoskeletonDeps(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
		wantErr  bool
	}{
		{
			name: "both postgres and nats",
			yaml: `
dependencies:
  - tentacular-postgres
  - tentacular-nats
  - redis
`,
			expected: []string{"tentacular-postgres", "tentacular-nats"},
		},
		{
			name: "postgres only",
			yaml: `
dependencies:
  - tentacular-postgres
  - external-api
`,
			expected: []string{"tentacular-postgres"},
		},
		{
			name: "no tentacular deps",
			yaml: `
dependencies:
  - redis
  - mongodb
`,
			expected: nil,
		},
		{
			name:     "no dependencies key",
			yaml:     `name: my-workflow`,
			expected: nil,
		},
		{
			name:     "empty yaml",
			yaml:     "",
			expected: nil,
		},
		{
			name:    "invalid yaml",
			yaml:    "{{invalid yaml",
			wantErr: true,
		},
		{
			name: "future tentacular services",
			yaml: `
dependencies:
  - tentacular-postgres
  - tentacular-nats
  - tentacular-rustfs
`,
			expected: []string{"tentacular-postgres", "tentacular-nats", "tentacular-rustfs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := DetectExoskeletonDeps(tt.yaml)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(deps) != len(tt.expected) {
				t.Fatalf("expected %d deps, got %d: %v", len(tt.expected), len(deps), deps)
			}
			for i, d := range deps {
				if d != tt.expected[i] {
					t.Errorf("dep[%d]: got %s, want %s", i, d, tt.expected[i])
				}
			}
		})
	}
}

func TestContainsDep(t *testing.T) {
	deps := []string{"tentacular-postgres", "tentacular-nats", "redis"}

	if !containsDep(deps, "tentacular-postgres") {
		t.Error("should contain tentacular-postgres")
	}
	if !containsDep(deps, "redis") {
		t.Error("should contain redis")
	}
	if containsDep(deps, "nonexistent") {
		t.Error("should not contain nonexistent")
	}
	if containsDep(nil, "tentacular-postgres") {
		t.Error("nil list should not contain anything")
	}
}
