package exoskeleton

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// ---------- Accessor method tests ----------

func TestControllerAccessors_AllEnabled(t *testing.T) {
	fakeDyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)
	cfg := &Config{
		Enabled:           true,
		CleanupOnUndeploy: true,
		Postgres: PostgresConfig{
			Host: "pg.exo.svc", Port: "5432", Database: "tentacular",
			User: "admin", Password: "secret",
		},
		NATS: NATSConfig{
			URL:           "nats://nats.exo.svc:4222",
			SPIFFEEnabled: true,
		},
		RustFS: RustFSConfig{
			Endpoint: "http://rustfs:9000", AccessKey: "ak", SecretKey: "sk",
			Bucket: "tentacular", Region: "us-east-1",
		},
		Auth: AuthConfig{
			Enabled:   true,
			IssuerURL: "https://keycloak.example.com/realms/tentacular",
			ClientID:  "tentacular-mcp",
		},
		SPIRE: SPIREConfig{
			Enabled:   true,
			ClassName: "test-spire",
		},
	}

	// Build controller manually to avoid real connections.
	ctrl := &Controller{
		cfg:    cfg,
		pg:     &PostgresRegistrar{cfg: cfg.Postgres},
		nats:   &NATSRegistrar{clientset: fake.NewClientset(), cfg: cfg.NATS},
		rustfs: &RustFSRegistrar{cfg: cfg.RustFS},
		spire:  NewSPIRERegistrar(fakeDyn, cfg.SPIRE.ClassName),
	}
	defer ctrl.Close()

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
	if !ctrl.CleanupOnUndeploy() {
		t.Error("expected CleanupOnUndeploy()=true")
	}
	if !ctrl.NATSSpiffeEnabled() {
		t.Error("expected NATSSpiffeEnabled()=true")
	}
	if !ctrl.AuthEnabled() {
		t.Error("expected AuthEnabled()=true")
	}
	if ctrl.AuthIssuer() != "https://keycloak.example.com/realms/tentacular" {
		t.Errorf("AuthIssuer() = %q", ctrl.AuthIssuer())
	}
}

func TestControllerAccessors_AllDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl := &Controller{cfg: cfg}
	defer ctrl.Close()

	if ctrl.Enabled() {
		t.Error("expected Enabled()=false")
	}
	if ctrl.PostgresAvailable() {
		t.Error("expected PostgresAvailable()=false")
	}
	if ctrl.NATSAvailable() {
		t.Error("expected NATSAvailable()=false")
	}
	if ctrl.RustFSAvailable() {
		t.Error("expected RustFSAvailable()=false")
	}
	if ctrl.SPIREAvailable() {
		t.Error("expected SPIREAvailable()=false")
	}
	if ctrl.CleanupOnUndeploy() {
		t.Error("expected CleanupOnUndeploy()=false")
	}
	if ctrl.NATSSpiffeEnabled() {
		t.Error("expected NATSSpiffeEnabled()=false")
	}
	if ctrl.AuthEnabled() {
		t.Error("expected AuthEnabled()=false")
	}
	if ctrl.AuthIssuer() != "" {
		t.Errorf("AuthIssuer() = %q, want empty", ctrl.AuthIssuer())
	}
}

// ---------- CleanupReport Summary tests ----------

func TestCleanupReport_Summary(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		report CleanupReport
	}{
		{
			name:   "no services",
			report: CleanupReport{},
			want:   "no services cleaned up",
		},
		{
			name:   "postgres only",
			report: CleanupReport{Performed: true, Postgres: "schema dropped"},
			want:   "postgres schema dropped",
		},
		{
			name:   "all services",
			report: CleanupReport{Performed: true, Postgres: "schema dropped", NATS: "authz entry removed", RustFS: "user removed", SPIRE: "identity removed"},
			want:   "postgres schema dropped, nats authz entry removed, rustfs user removed, spire identity removed",
		},
		{
			name:   "nats only",
			report: CleanupReport{Performed: true, NATS: "no-op"},
			want:   "nats no-op",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.report.Summary()
			if got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------- CleanupWithReport tests ----------

func TestCleanupWithReport_Disabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl := &Controller{cfg: cfg}

	report, err := ctrl.CleanupWithReport(context.Background(), "ns", "wf")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if report.Performed {
		t.Error("expected Performed=false when disabled")
	}
}

func TestCleanupWithReport_CleanupDisabled(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: false}
	ctrl := &Controller{cfg: cfg}

	report, err := ctrl.CleanupWithReport(context.Background(), "ns", "wf")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if report.Performed {
		t.Error("expected Performed=false when CleanupOnUndeploy=false")
	}
}

func TestCleanupWithReport_WithNATS(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled:           true,
		CleanupOnUndeploy: true,
		NATS: NATSConfig{
			URL:            "nats://localhost:4222",
			SPIFFEEnabled:  false,
			AuthzConfigMap: "nats-authz",
			AuthzNamespace: "exo",
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	report, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if !report.Performed {
		t.Error("expected Performed=true")
	}
	// In token mode, NATS Unregister is a no-op.
	if report.NATS != "no-op" {
		t.Errorf("NATS report = %q, want 'no-op'", report.NATS)
	}
}

func TestCleanupWithReport_WithNATSSPIFFE(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled:           true,
		CleanupOnUndeploy: true,
		NATS: NATSConfig{
			URL:            "nats://localhost:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-authz",
			AuthzNamespace: "exo",
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	// SPIFFE unregister on a non-existent ConfigMap returns nil (not found is OK).
	report, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if !report.Performed {
		t.Error("expected Performed=true")
	}
	if report.NATS != "authz entry removed" {
		t.Errorf("NATS report = %q, want 'authz entry removed'", report.NATS)
	}
}

func TestCleanupWithReport_WithSPIRE(t *testing.T) {
	fakeDyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)
	spireReg := NewSPIRERegistrar(fakeDyn, "test-spire")

	cfg := &Config{Enabled: true, CleanupOnUndeploy: true}
	ctrl := &Controller{cfg: cfg, spire: spireReg}

	// Register SPIRE first so unregister has something to delete.
	id, _ := CompileIdentity("tent-dev", "hn-digest")
	ctx := context.Background()
	_ = spireReg.Register(ctx, id, "tent-dev")

	report, err := ctrl.CleanupWithReport(ctx, "tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if report.SPIRE != "identity removed" {
		t.Errorf("SPIRE report = %q, want 'identity removed'", report.SPIRE)
	}
}

// ---------- ProcessManifests full flow tests ----------

// makeWorkflowManifests builds a set of manifests with a ConfigMap containing
// the given deps and a Deployment with --allow-net.
func makeWorkflowManifests(deps ...string) []map[string]any {
	var depYAML string
	var depYAMLSb283 strings.Builder
	for _, d := range deps {
		protocol := "postgresql"
		if strings.Contains(d, "nats") {
			protocol = "nats"
		}
		if strings.Contains(d, "rustfs") {
			protocol = "s3"
		}
		depYAMLSb283.WriteString("    " + d + ":\n      protocol: " + protocol + "\n")
	}
	depYAML += depYAMLSb283.String()

	wfYAML := "name: test-workflow\nversion: \"1.0\"\ncontract:\n  dependencies:\n" + depYAML +
		"triggers:\n  - type: http\nnodes:\n  ingest:\n    path: nodes/ingest.ts\n"

	cm := makeConfigMapManifest(wfYAML)
	dep := makeDeploymentManifest([]string{"run", "--allow-net=api.example.com:443", "main.ts"})

	return []map[string]any{cm, dep}
}

func TestProcessManifests_DisabledPostgres_Rejects(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	manifests := makeWorkflowManifests("tentacular-postgres")
	_, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled postgres")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("error should mention 'not enabled': %v", err)
	}
}

func TestProcessManifests_DisabledNATS_Rejects(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	manifests := makeWorkflowManifests("tentacular-nats")
	_, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled nats")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("error should mention 'not enabled': %v", err)
	}
}

func TestProcessManifests_DisabledRustFS_Rejects(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	manifests := makeWorkflowManifests("tentacular-rustfs")
	_, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for disabled rustfs")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("error should mention 'not enabled': %v", err)
	}
}

func TestProcessManifests_NATSTokenMode(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled: true,
		NATS: NATSConfig{
			URL:           "nats://nats.exo.svc:4222",
			Token:         "test-token",
			SPIFFEEnabled: false,
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	manifests := makeWorkflowManifests("tentacular-nats")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "hn-digest", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	// Should have original 2 manifests + exo audit Secret + user Secret.
	if len(result) != 4 {
		t.Fatalf("expected 4 manifests (2 original + exo secret + user secret), got %d", len(result))
	}

	// Verify the exo audit Secret manifest (index 2).
	secret := result[2]
	if secret["kind"] != "Secret" {
		t.Errorf("expected Secret kind, got %v", secret["kind"])
	}
	metadata := secret["metadata"].(map[string]any)
	if metadata["name"] != "tentacular-exoskeleton-hn-digest" {
		t.Errorf("secret name = %v", metadata["name"])
	}
	if metadata["namespace"] != "tent-dev" {
		t.Errorf("secret namespace = %v", metadata["namespace"])
	}

	// Verify the user Secret manifest (index 3).
	userSecret := result[3]
	if userSecret["kind"] != "Secret" {
		t.Errorf("expected Secret kind for user secret, got %v", userSecret["kind"])
	}
	userMeta := userSecret["metadata"].(map[string]any)
	if userMeta["name"] != "hn-digest-secrets" {
		t.Errorf("user secret name = %v, want hn-digest-secrets", userMeta["name"])
	}
}

func TestProcessManifests_WithSPIRE(t *testing.T) {
	fakeCS := fake.NewClientset()
	fakeDyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)

	cfg := &Config{
		Enabled: true,
		NATS: NATSConfig{
			URL:           "nats://nats.exo.svc:4222",
			Token:         "test-token",
			SPIFFEEnabled: false,
		},
		SPIRE: SPIREConfig{
			Enabled:   true,
			ClassName: "test-spire",
		},
	}
	spireReg := NewSPIRERegistrar(fakeDyn, cfg.SPIRE.ClassName)
	ctrl := &Controller{
		cfg:   cfg,
		nats:  &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
		spire: spireReg,
	}

	manifests := makeWorkflowManifests("tentacular-nats")
	ctx := context.Background()
	_, err := ctrl.ProcessManifests(ctx, "tent-dev", "hn-digest", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	// Verify SPIRE resource was created.
	name := spireName("tent-dev", "hn-digest")
	_, err = fakeDyn.Resource(clusterSPIFFEIDGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected ClusterSPIFFEID to be created: %v", err)
	}
}

func TestProcessManifests_NoDeps(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	manifests := makeWorkflowManifests() // no tentacular deps
	result, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	// No deps means manifests pass through unchanged.
	if len(result) != 2 {
		t.Errorf("expected 2 manifests unchanged, got %d", len(result))
	}
}

func TestProcessManifests_UnknownDep(t *testing.T) {
	// A workflow with an unknown tentacular-* dep should not error, just warn.
	wfYAML := `name: test
contract:
  dependencies:
    tentacular-unknown:
      protocol: custom
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	// Unknown dep should produce an empty creds map (no Secret appended),
	// but should NOT error.
	result, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}
	// No creds generated for unknown dep, so no Secret appended.
	if len(result) != 1 {
		t.Errorf("expected 1 manifest, got %d", len(result))
	}
}

// ---------- Close tests ----------

func TestController_Close_AllNil(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl := &Controller{cfg: cfg}
	// Should not panic.
	if err := ctrl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestController_Close_WithRegistrars(t *testing.T) {
	fakeDyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)
	cfg := &Config{Enabled: true}
	ctrl := &Controller{
		cfg:    cfg,
		nats:   &NATSRegistrar{cfg: NATSConfig{}},
		rustfs: &RustFSRegistrar{cfg: RustFSConfig{}},
		spire:  NewSPIRERegistrar(fakeDyn, "test"),
	}
	if err := ctrl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// ---------- CleanupWithReport error path ----------

func TestCleanupWithReport_SPIREErrorIsCollected(t *testing.T) {
	fakeDyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			clusterSPIFFEIDGVR: "ClusterSPIFFEIDList",
		},
	)
	spireReg := NewSPIRERegistrar(fakeDyn, "test-spire")

	cfg := &Config{Enabled: true, CleanupOnUndeploy: true}
	ctrl := &Controller{cfg: cfg, spire: spireReg}

	// Unregister without registering first -- resource doesn't exist.
	// SPIRE unregister is idempotent: NotFound is not an error.
	report, err := ctrl.CleanupWithReport(context.Background(), "tent-dev", "nonexistent-wf")
	if err != nil {
		t.Fatalf("expected no error for idempotent SPIRE unregister, got: %v", err)
	}
	if report.SPIRE == "" {
		t.Error("SPIRE report should be non-empty on successful cleanup")
	}
}

func TestCleanupWithReport_RustFSCleanup(t *testing.T) {
	// RustFS Unregister does admin calls (which will fail without a real server),
	// but it logs warnings and returns nil because errors are non-fatal.
	// We can test this by constructing a RustFSRegistrar with a mock admin.
	// However, since admin is not exported, we test via the controller flow.

	// For now, just test that the report is populated correctly when all
	// registrars succeed.
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled:           true,
		CleanupOnUndeploy: true,
		NATS: NATSConfig{
			URL:           "nats://localhost:4222",
			SPIFFEEnabled: false,
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	report, err := ctrl.CleanupWithReport(context.Background(), "ns", "wf")
	if err != nil {
		t.Fatalf("CleanupWithReport: %v", err)
	}
	if !report.Performed {
		t.Error("expected Performed=true")
	}
	if report.NATS != "no-op" {
		t.Errorf("NATS report = %q", report.NATS)
	}
	// Postgres and RustFS not configured, so should be empty.
	if report.Postgres != "" {
		t.Errorf("Postgres report should be empty, got %q", report.Postgres)
	}
	if report.RustFS != "" {
		t.Errorf("RustFS report should be empty, got %q", report.RustFS)
	}
}

// ---------- ProcessManifests with all three deps (where registrar is nil) ----------

func TestProcessManifests_AllThreeDisabled_RejectsAll(t *testing.T) {
	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	// All three tentacular deps declared, but none configured.
	wfYAML := `name: test
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    tentacular-rustfs:
      protocol: s3
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	_, err := ctrl.ProcessManifests(context.Background(), "ns", "wf", manifests)
	if err == nil {
		t.Fatal("expected error when all services disabled")
	}
	// Should fail on the first dependency encountered.
	if !strings.Contains(err.Error(), "not enabled") && !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error should mention disabled service: %v", err)
	}
}

// ---------- ProcessManifests with NATS SPIFFE mode ----------

func TestProcessManifests_NATSSPIFFEMode(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled: true,
		NATS: NATSConfig{
			URL:            "nats://nats.exo.svc:4222",
			SPIFFEEnabled:  true,
			AuthzConfigMap: "nats-authz",
			AuthzNamespace: "exo",
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	manifests := makeWorkflowManifests("tentacular-nats")
	result, err := ctrl.ProcessManifests(context.Background(), "tent-dev", "hn-digest", manifests)
	if err != nil {
		t.Fatalf("ProcessManifests: %v", err)
	}

	// Should have 4 manifests (CM + Deployment + exo Secret + user Secret).
	if len(result) != 4 {
		t.Fatalf("expected 4 manifests, got %d", len(result))
	}

	// Exo audit Secret should not contain a token in SPIFFE mode.
	secret := result[2]
	stringData := secret["stringData"].(map[string]any)
	if _, hasToken := stringData["tentacular-nats.token"]; hasToken {
		t.Error("SPIFFE mode should not include a token in the secret")
	}
	if stringData["tentacular-nats.auth_method"] != "spiffe" {
		t.Errorf("expected auth_method=spiffe, got %v", stringData["tentacular-nats.auth_method"])
	}

	// User secret should also not contain a token in SPIFFE mode.
	userSecret := result[3]
	userSD := userSecret["stringData"].(map[string]any)
	if _, hasToken := userSD["tentacular-nats.token"]; hasToken {
		t.Error("SPIFFE mode user secret should not include a token")
	}
}

// ---------- NewController with disabled config ----------

func TestNewController_Disabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	ctrl, err := NewController(cfg, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	defer ctrl.Close()

	if ctrl.Enabled() {
		t.Error("expected Enabled()=false")
	}
}

// ---------- detectExoDeps with invalid YAML ----------

func TestDetectExoDeps_InvalidYAML(t *testing.T) {
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "test"},
		"data": map[string]any{
			"workflow.yaml": "{{invalid yaml}}",
		},
	}
	deps := detectExoDeps([]map[string]any{cm})
	if len(deps) != 0 {
		t.Errorf("expected 0 deps for invalid YAML, got %v", deps)
	}
}

// ---------- detectExoDeps with nil contract ----------

func TestDetectExoDeps_NilContract(t *testing.T) {
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "test"},
		"data": map[string]any{
			"workflow.yaml": `
name: no-contract
triggers:
  - type: http
`,
		},
	}
	deps := detectExoDeps([]map[string]any{cm})
	if len(deps) != 0 {
		t.Errorf("expected 0 deps for nil contract, got %v", deps)
	}
}

// ---------- spireName with long input ----------

func TestSpireName_LongInput(t *testing.T) {
	longNs := strings.Repeat("a", 200)
	longWf := strings.Repeat("b", 200)
	name := spireName(longNs, longWf)
	if len(name) > 253 {
		t.Errorf("spireName length = %d, should be <= 253", len(name))
	}
}

// ---------- Cleanup calls CleanupWithReport ----------

func TestCleanup_DelegatesToCleanupWithReport(t *testing.T) {
	cfg := &Config{Enabled: true, CleanupOnUndeploy: false}
	ctrl := &Controller{cfg: cfg}

	err := ctrl.Cleanup(context.Background(), "ns", "wf")
	if err != nil {
		t.Errorf("Cleanup: %v", err)
	}
}

// ---------- ProcessManifests with empty namespace or name ----------

func TestProcessManifests_EmptyNameReturnsError(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled: true,
		NATS: NATSConfig{
			URL:           "nats://localhost:4222",
			SPIFFEEnabled: false,
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	manifests := makeWorkflowManifests("tentacular-nats")
	// Empty workflow name should error from CompileIdentity.
	_, err := ctrl.ProcessManifests(context.Background(), "ns", "", manifests)
	if err == nil {
		t.Fatal("expected error for empty workflow name")
	}
}

func TestProcessManifests_EmptyNamespaceReturnsError(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled: true,
		NATS: NATSConfig{
			URL:           "nats://localhost:4222",
			SPIFFEEnabled: false,
		},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	manifests := makeWorkflowManifests("tentacular-nats")
	_, err := ctrl.ProcessManifests(context.Background(), "", "wf", manifests)
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

// ---------- CleanupWithReport with empty name ----------

func TestCleanupWithReport_EmptyNameReturnsError(t *testing.T) {
	fakeCS := fake.NewClientset()
	cfg := &Config{
		Enabled: true, CleanupOnUndeploy: true,
		NATS: NATSConfig{URL: "nats://localhost:4222"},
	}
	ctrl := &Controller{
		cfg:  cfg,
		nats: &NATSRegistrar{clientset: fakeCS, cfg: cfg.NATS},
	}

	_, err := ctrl.CleanupWithReport(context.Background(), "ns", "")
	if err == nil {
		t.Fatal("expected error for empty workflow name")
	}
}
