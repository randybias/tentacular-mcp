package exoskeleton

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// --- Mock registrars for interface-based testing ---

type mockPG struct {
	creds           *PostgresCreds
	registerErr     error
	unregisterErr   error
	registerCalls   []Identity
	unregisterCalls []Identity
	closed          bool
}

func newMockPG() *mockPG {
	return &mockPG{
		creds: &PostgresCreds{
			Host: "pg.test", Port: "5432", Database: "testdb",
			User: "tn_ns_wf", Password: "gen-pw", Schema: "tn_ns_wf",
			Protocol: "postgresql",
		},
	}
}

func (m *mockPG) Register(_ context.Context, id Identity) (*PostgresCreds, error) {
	m.registerCalls = append(m.registerCalls, id)
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.creds, nil
}

func (m *mockPG) Unregister(_ context.Context, id Identity) error {
	m.unregisterCalls = append(m.unregisterCalls, id)
	return m.unregisterErr
}

func (m *mockPG) Close() { m.closed = true }

type mockNATS struct {
	creds           *NATSCreds
	registerErr     error
	unregisterErr   error
	registerCalls   []Identity
	unregisterCalls []Identity
	closed          bool
}

func newMockNATS() *mockNATS {
	return &mockNATS{
		creds: &NATSCreds{
			URL: "nats://test:4222", Token: "nats-tok",
			SubjectPrefix: "tentacular.ns.wf.>", Protocol: "nats",
			AuthMethod: "token",
		},
	}
}

func (m *mockNATS) Register(_ context.Context, id Identity) (*NATSCreds, error) {
	m.registerCalls = append(m.registerCalls, id)
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.creds, nil
}

func (m *mockNATS) Unregister(_ context.Context, id Identity) error {
	m.unregisterCalls = append(m.unregisterCalls, id)
	return m.unregisterErr
}

func (m *mockNATS) Close() { m.closed = true }

type mockRustFS struct {
	creds           *RustFSCreds
	registerErr     error
	unregisterErr   error
	registerCalls   []Identity
	unregisterCalls []Identity
	closed          bool
}

func newMockRustFS() *mockRustFS {
	return &mockRustFS{
		creds: &RustFSCreds{
			Endpoint: "http://minio:9000", AccessKey: "ak", SecretKey: "sk",
			Bucket: "tentacular", Prefix: "ns/ns/tentacles/wf/",
			Region: "us-east-1", Protocol: "s3",
		},
	}
}

func (m *mockRustFS) Register(_ context.Context, id Identity) (*RustFSCreds, error) {
	m.registerCalls = append(m.registerCalls, id)
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.creds, nil
}

func (m *mockRustFS) Unregister(_ context.Context, id Identity) error {
	m.unregisterCalls = append(m.unregisterCalls, id)
	return m.unregisterErr
}

func (m *mockRustFS) Close() { m.closed = true }

type mockSPIRE struct {
	registerErr     error
	unregisterErr   error
	registerCalls   []string // namespace args
	unregisterCalls []string
	closed          bool
}

func (m *mockSPIRE) Register(_ context.Context, _ Identity, namespace string) error {
	m.registerCalls = append(m.registerCalls, namespace)
	return m.registerErr
}

func (m *mockSPIRE) Unregister(_ context.Context, _ Identity, namespace string) error {
	m.unregisterCalls = append(m.unregisterCalls, namespace)
	return m.unregisterErr
}

func (m *mockSPIRE) Close() { m.closed = true }

// --- Helper to build manifests with workflow dependencies ---

func manifestsWithDeps(deps ...string) []map[string]any {
	var b strings.Builder
	b.WriteString("contract:\n  dependencies:\n")
	for _, d := range deps {
		b.WriteString("    ")
		b.WriteString(d)
		b.WriteString(":\n      protocol: test\n")
	}
	return []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "test-wf", "namespace": "tent-dev"},
			"data":       map[string]any{"workflow.yaml": b.String()},
		},
		{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]any{"name": "test-wf", "namespace": "tent-dev"},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{
								"name":  "main",
								"image": "test:latest",
								"args":  []any{"--allow-net=none"},
							},
						},
					},
				},
			},
		},
	}
}

// --- Existing tests ---

func TestDetectExoDeps(t *testing.T) {
	tests := []struct {
		name      string
		manifests []map[string]any
		wantDeps  int
	}{
		{
			name: "no configmap",
			manifests: []map[string]any{
				{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "test"}},
			},
			wantDeps: 0,
		},
		{
			name: "configmap without workflow.yaml",
			manifests: []map[string]any{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{"name": "test"},
					"data":       map[string]any{"other.yaml": "foo: bar"},
				},
			},
			wantDeps: 0,
		},
		{
			name: "workflow with tentacular deps",
			manifests: []map[string]any{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{"name": "test"},
					"data": map[string]any{
						"workflow.yaml": `
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    some-other-dep:
      protocol: http
`,
					},
				},
			},
			wantDeps: 2,
		},
		{
			name: "workflow without tentacular deps",
			manifests: []map[string]any{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{"name": "test"},
					"data": map[string]any{
						"workflow.yaml": `
contract:
  dependencies:
    redis:
      protocol: redis
`,
					},
				},
			},
			wantDeps: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := detectExoDeps(tt.manifests)
			if len(deps) != tt.wantDeps {
				t.Errorf("got %d deps, want %d: %v", len(deps), tt.wantDeps, deps)
			}
		})
	}
}

func TestControllerDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl, err := NewController(cfg, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	defer ctrl.Close()

	if ctrl.Enabled() {
		t.Error("expected Enabled()=false")
	}

	// ProcessManifests should pass through unchanged
	manifests := []map[string]any{
		{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "test"}},
	}
	result, err := ctrl.ProcessManifests(context.TODO(), "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(result) != len(manifests) {
		t.Errorf("got %d manifests, want %d", len(result), len(manifests))
	}

	// Cleanup should be a no-op
	if err := ctrl.Cleanup(context.TODO(), "ns", "wf"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}

// --- Mock-based tests enabled by interface-based registrar design ---

func TestNewControllerWithDeps(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	nats := newMockNATS()
	rustfs := newMockRustFS()
	spire := &mockSPIRE{}

	ctrl := NewControllerWithDeps(cfg, pg, nats, rustfs, spire)

	if !ctrl.Enabled() {
		t.Error("expected Enabled()=true")
	}
	if !ctrl.PostgresAvailable() {
		t.Error("expected PostgresAvailable()=true")
	}
	if !ctrl.NATSAvailable() {
		t.Error("expected NATSAvailable()=true")
	}
	if !ctrl.RustFSAvailable() {
		t.Error("expected RustFSAvailable()=true")
	}
	if !ctrl.SPIREAvailable() {
		t.Error("expected SPIREAvailable()=true")
	}
}

func TestProcessManifestsWithMockPostgres(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, nil)

	manifests := manifestsWithDeps("tentacular-postgres")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	if len(pg.registerCalls) != 1 {
		t.Fatalf("expected 1 postgres register call, got %d", len(pg.registerCalls))
	}
	if pg.registerCalls[0].Namespace != "tent-dev" {
		t.Errorf("expected namespace tent-dev, got %s", pg.registerCalls[0].Namespace)
	}

	// Should have added a Secret manifest
	if len(result) <= len(manifests) {
		t.Error("expected Secret manifest to be appended")
	}
}

func TestProcessManifestsWithMockNATS(t *testing.T) {
	cfg := &Config{Enabled: true}
	nats := newMockNATS()
	ctrl := NewControllerWithDeps(cfg, nil, nats, nil, nil)

	manifests := manifestsWithDeps("tentacular-nats")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	if len(nats.registerCalls) != 1 {
		t.Fatalf("expected 1 nats register call, got %d", len(nats.registerCalls))
	}

	if len(result) <= len(manifests) {
		t.Error("expected Secret manifest to be appended")
	}
}

func TestProcessManifestsWithMockRustFS(t *testing.T) {
	cfg := &Config{Enabled: true}
	rustfs := newMockRustFS()
	ctrl := NewControllerWithDeps(cfg, nil, nil, rustfs, nil)

	manifests := manifestsWithDeps("tentacular-rustfs")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	if len(rustfs.registerCalls) != 1 {
		t.Fatalf("expected 1 rustfs register call, got %d", len(rustfs.registerCalls))
	}

	if len(result) <= len(manifests) {
		t.Error("expected Secret manifest to be appended")
	}
}

func TestProcessManifestsPostgresRegistrationError(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	pg.registerErr = errors.New("connection refused")
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, nil)

	manifests := manifestsWithDeps("tentacular-postgres")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err == nil {
		t.Fatal("expected error on postgres registration failure")
	}
	if !strings.Contains(err.Error(), "postgres registration") {
		t.Errorf("expected 'postgres registration' in error, got: %v", err)
	}
}

func TestProcessManifestsNATSRegistrationError(t *testing.T) {
	cfg := &Config{Enabled: true}
	nats := newMockNATS()
	nats.registerErr = errors.New("nats unreachable")
	ctrl := NewControllerWithDeps(cfg, nil, nats, nil, nil)

	manifests := manifestsWithDeps("tentacular-nats")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err == nil {
		t.Fatal("expected error on nats registration failure")
	}
	if !strings.Contains(err.Error(), "nats registration") {
		t.Errorf("expected 'nats registration' in error, got: %v", err)
	}
}

func TestProcessManifestsRustFSRegistrationError(t *testing.T) {
	cfg := &Config{Enabled: true}
	rustfs := newMockRustFS()
	rustfs.registerErr = errors.New("rustfs unreachable")
	ctrl := NewControllerWithDeps(cfg, nil, nil, rustfs, nil)

	manifests := manifestsWithDeps("tentacular-rustfs")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err == nil {
		t.Fatal("expected error on rustfs registration failure")
	}
	if !strings.Contains(err.Error(), "rustfs registration") {
		t.Errorf("expected 'rustfs registration' in error, got: %v", err)
	}
}

func TestProcessManifestsMissingRegistrar(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := NewControllerWithDeps(cfg, nil, nil, nil, nil)

	manifests := manifestsWithDeps("tentacular-postgres")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err == nil {
		t.Fatal("expected error when postgres dep requested but registrar nil")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' in error, got: %v", err)
	}
}

func TestProcessManifestsNoDeps(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, nil)

	manifests := manifestsWithDeps("redis")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(pg.registerCalls) != 0 {
		t.Error("postgres should not be called when no tentacular deps")
	}
	if len(result) != len(manifests) {
		t.Error("manifests should be unchanged when no tentacular deps")
	}
}

func TestProcessManifestsSPIRECalledOnRegistration(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	spire := &mockSPIRE{}
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, spire)

	manifests := manifestsWithDeps("tentacular-postgres")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	if len(spire.registerCalls) != 1 {
		t.Fatalf("expected 1 SPIRE register call, got %d", len(spire.registerCalls))
	}
	if spire.registerCalls[0] != "tent-dev" {
		t.Errorf("SPIRE register namespace = %s, want tent-dev", spire.registerCalls[0])
	}
}

func TestProcessManifestsSPIREErrorNonFatal(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	spire := &mockSPIRE{registerErr: errors.New("spire unavailable")}
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, spire)

	manifests := manifestsWithDeps("tentacular-postgres")
	_, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "mywf", manifests)
	if err != nil {
		t.Fatalf("SPIRE error should be non-fatal, got: %v", err)
	}
}

func TestCleanupWithMocks(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: true}
	pg := newMockPG()
	nats := newMockNATS()
	rustfs := newMockRustFS()
	spire := &mockSPIRE{}
	ctrl := NewControllerWithDeps(cfg, pg, nats, rustfs, spire)

	report, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "mywf")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if !report.Performed {
		t.Error("expected cleanup to be performed")
	}
	if len(pg.unregisterCalls) != 1 {
		t.Errorf("expected 1 postgres unregister, got %d", len(pg.unregisterCalls))
	}
	if len(nats.unregisterCalls) != 1 {
		t.Errorf("expected 1 nats unregister, got %d", len(nats.unregisterCalls))
	}
	if len(rustfs.unregisterCalls) != 1 {
		t.Errorf("expected 1 rustfs unregister, got %d", len(rustfs.unregisterCalls))
	}
	if len(spire.unregisterCalls) != 1 {
		t.Errorf("expected 1 spire unregister, got %d", len(spire.unregisterCalls))
	}
}

func TestCleanupPartialFailure(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: true}
	pg := newMockPG()
	nats := newMockNATS()
	nats.unregisterErr = errors.New("nats unreachable")
	ctrl := NewControllerWithDeps(cfg, pg, nats, nil, nil)

	_, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "mywf")
	if err == nil {
		t.Fatal("expected error on partial failure")
	}
	if !strings.Contains(err.Error(), "nats") {
		t.Errorf("expected nats mentioned in error: %v", err)
	}
	// Postgres should still be cleaned up despite NATS failure
	if len(pg.unregisterCalls) != 1 {
		t.Error("postgres unregister should still be called on partial failure")
	}
}

func TestCleanupDisabled(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: false}
	pg := newMockPG()
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, nil)

	report, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "mywf")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if report.Performed {
		t.Error("cleanup should not be performed when CleanupOnUndeploy=false")
	}
	if len(pg.unregisterCalls) != 0 {
		t.Error("nothing should be called when cleanup disabled")
	}
}

func TestControllerClose(t *testing.T) {
	cfg := &Config{Enabled: true}
	pg := newMockPG()
	nats := newMockNATS()
	rustfs := newMockRustFS()
	spire := &mockSPIRE{}
	ctrl := NewControllerWithDeps(cfg, pg, nats, rustfs, spire)

	ctrl.Close()

	if !pg.closed {
		t.Error("postgres should be closed")
	}
	if !nats.closed {
		t.Error("nats should be closed")
	}
	if !rustfs.closed {
		t.Error("rustfs should be closed")
	}
	if !spire.closed {
		t.Error("spire should be closed")
	}
}

// --- ServiceInfo tests ---

func TestServiceInfoDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl := NewControllerWithDeps(cfg, nil, nil, nil, nil)

	info := ctrl.ServiceInfo()
	if info != nil {
		t.Error("expected ServiceInfo to return nil when disabled")
	}
}

func TestServiceInfoAllServicesAvailable(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Postgres: PostgresConfig{
			Host: "pg.example.com",
			Port: "5432",
		},
		NATS: NATSConfig{
			URL:           "nats://nats.example.com:4222",
			SPIFFEEnabled: true,
		},
		RustFS: RustFSConfig{
			Endpoint: "http://rustfs.example.com:9000",
		},
		Auth: AuthConfig{
			Enabled:   true,
			IssuerURL: "https://keycloak.example.com/realms/tentacular",
			ClientID:  "my-client",
		},
	}
	pg := newMockPG()
	nats := newMockNATS()
	rustfs := newMockRustFS()
	spire := &mockSPIRE{}
	ctrl := NewControllerWithDeps(cfg, pg, nats, rustfs, spire)

	info := ctrl.ServiceInfo()
	if info == nil {
		t.Fatal("expected non-nil ServiceInfo")
	}
	if !info.Enabled {
		t.Error("expected Enabled=true")
	}

	if len(info.Services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(info.Services))
	}

	svcByName := map[string]k8s.ExoskeletonServiceInfo{}
	for _, s := range info.Services {
		svcByName[s.Name] = s
	}

	// Postgres
	pgSvc, ok := svcByName["postgres"]
	if !ok {
		t.Fatal("expected postgres service")
	}
	if pgSvc.Host != "pg.example.com" {
		t.Errorf("postgres host = %q", pgSvc.Host)
	}
	if pgSvc.Port != "5432" {
		t.Errorf("postgres port = %q", pgSvc.Port)
	}
	if pgSvc.Protocol != "tcp" {
		t.Errorf("postgres protocol = %q", pgSvc.Protocol)
	}
	if !pgSvc.Available {
		t.Error("expected postgres Available=true")
	}

	// NATS
	natsSvc, ok := svcByName["nats"]
	if !ok {
		t.Fatal("expected nats service")
	}
	if natsSvc.Host != "nats.example.com" {
		t.Errorf("nats host = %q", natsSvc.Host)
	}
	if natsSvc.Port != "4222" {
		t.Errorf("nats port = %q", natsSvc.Port)
	}
	if !natsSvc.Available {
		t.Error("expected nats Available=true")
	}
	if !natsSvc.SPIFFEEnabled {
		t.Error("expected nats SPIFFEEnabled=true")
	}

	// RustFS
	rustfsSvc, ok := svcByName["rustfs"]
	if !ok {
		t.Fatal("expected rustfs service")
	}
	if rustfsSvc.Host != "rustfs.example.com" {
		t.Errorf("rustfs host = %q", rustfsSvc.Host)
	}
	if rustfsSvc.Port != "9000" {
		t.Errorf("rustfs port = %q", rustfsSvc.Port)
	}
	if !rustfsSvc.Available {
		t.Error("expected rustfs Available=true")
	}

	// SPIRE
	spireSvc, ok := svcByName["spire"]
	if !ok {
		t.Fatal("expected spire service")
	}
	if !spireSvc.Available {
		t.Error("expected spire Available=true")
	}

	// Auth
	if !info.Auth.Enabled {
		t.Error("expected auth Enabled=true")
	}
	if info.Auth.Issuer != "https://keycloak.example.com/realms/tentacular" {
		t.Errorf("auth issuer = %q", info.Auth.Issuer)
	}
}

func TestServiceInfoPartialServices(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Postgres: PostgresConfig{
			Host: "pg.test",
			Port: "5432",
		},
		NATS: NATSConfig{
			URL: "nats://nats.test:4222",
		},
		RustFS: RustFSConfig{
			Endpoint: "http://rustfs.test:9000",
		},
	}
	// Only postgres registrar available, others nil
	pg := newMockPG()
	ctrl := NewControllerWithDeps(cfg, pg, nil, nil, nil)

	info := ctrl.ServiceInfo()
	if info == nil {
		t.Fatal("expected non-nil ServiceInfo")
	}

	for _, svc := range info.Services {
		switch svc.Name {
		case "postgres":
			if !svc.Available {
				t.Error("expected postgres Available=true")
			}
		case "nats":
			if svc.Available {
				t.Error("expected nats Available=false with nil registrar")
			}
		case "rustfs":
			if svc.Available {
				t.Error("expected rustfs Available=false with nil registrar")
			}
		case "spire":
			if svc.Available {
				t.Error("expected spire Available=false with nil registrar")
			}
		default:
			t.Errorf("unexpected service name: %s", svc.Name)
		}
	}
}

func TestServiceInfoNoAuth(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Auth:    AuthConfig{Enabled: false},
	}
	ctrl := NewControllerWithDeps(cfg, nil, nil, nil, nil)

	info := ctrl.ServiceInfo()
	if info == nil {
		t.Fatal("expected non-nil ServiceInfo")
	}
	if info.Auth.Enabled {
		t.Error("expected auth Enabled=false")
	}
	if info.Auth.Issuer != "" {
		t.Errorf("expected empty issuer, got %q", info.Auth.Issuer)
	}
}

func TestServiceInfoPartialAuthConfig(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Auth: AuthConfig{
			Enabled:   true,
			IssuerURL: "https://keycloak.example.com/realms/tentacular",
			// ClientID intentionally missing — AuthEnabled() should return false
		},
	}
	ctrl := NewControllerWithDeps(cfg, nil, nil, nil, nil)

	info := ctrl.ServiceInfo()
	if info == nil {
		t.Fatal("expected non-nil ServiceInfo")
	}
	if info.Auth.Enabled {
		t.Error("expected auth Enabled=false when ClientID is missing")
	}
	if info.Auth.Issuer != "" {
		t.Errorf("expected empty issuer when auth not fully enabled, got %q", info.Auth.Issuer)
	}
}
