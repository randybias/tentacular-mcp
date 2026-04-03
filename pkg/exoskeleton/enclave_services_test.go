package exoskeleton

import (
	"context"
	"errors"
	"testing"
)

// --- EnsureEnclaveServices ---

func TestEnsureEnclaveServices_EmptyServices_NoOp(t *testing.T) {
	c := NewControllerWithDeps(&Config{Enabled: true}, newMockPG(), newMockNATS(), newMockRustFS(), &mockSPIRE{})
	if err := c.EnsureEnclaveServices(context.Background(), "my-enclave", nil); err != nil {
		t.Errorf("expected nil for empty services, got: %v", err)
	}
	if err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{}); err != nil {
		t.Errorf("expected nil for empty services slice, got: %v", err)
	}
}

func TestEnsureEnclaveServices_ExoDisabled_NoError(t *testing.T) {
	// Per the design: when exoskeleton is disabled, EnsureEnclaveServices returns nil.
	c := NewControllerWithDeps(&Config{Enabled: false}, nil, nil, nil, nil)
	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres"})
	if err != nil {
		t.Errorf("expected nil when exoskeleton disabled (no-op), got: %v", err)
	}
}

func TestEnsureEnclaveServices_AllServicesAvailable(t *testing.T) {
	pg := newMockPG()
	nats := newMockNATS()
	rustfs := newMockRustFS()
	c := NewControllerWithDeps(&Config{Enabled: true}, pg, nats, rustfs, &mockSPIRE{})

	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres", "rustfs", "nats"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pg.ensureEnclaveCalls) != 1 {
		t.Errorf("postgres EnsureEnclave called %d times, want 1", len(pg.ensureEnclaveCalls))
	}
	if len(rustfs.ensureEnclaveCalls) != 1 {
		t.Errorf("rustfs EnsureEnclave called %d times, want 1", len(rustfs.ensureEnclaveCalls))
	}
	if len(nats.ensureEnclaveCalls) != 1 {
		t.Errorf("nats EnsureEnclave called %d times, want 1", len(nats.ensureEnclaveCalls))
	}
}

func TestEnsureEnclaveServices_PostgresNotAvailable_ReturnsError(t *testing.T) {
	c := NewControllerWithDeps(&Config{Enabled: true}, nil, newMockNATS(), newMockRustFS(), nil)
	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres"})
	if err == nil {
		t.Error("expected error when postgres not available, got nil")
	}
}

func TestEnsureEnclaveServices_NATSNotAvailable_ReturnsError(t *testing.T) {
	c := NewControllerWithDeps(&Config{Enabled: true}, newMockPG(), nil, newMockRustFS(), nil)
	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"nats"})
	if err == nil {
		t.Error("expected error when nats not available, got nil")
	}
}

func TestEnsureEnclaveServices_RustFSNotAvailable_ReturnsError(t *testing.T) {
	c := NewControllerWithDeps(&Config{Enabled: true}, newMockPG(), newMockNATS(), nil, nil)
	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"rustfs"})
	if err == nil {
		t.Error("expected error when rustfs not available, got nil")
	}
}

func TestEnsureEnclaveServices_PostgresEnsureEnclaveFails(t *testing.T) {
	pg := newMockPG()
	pg.ensureEnclaveErr = errors.New("pg unavailable")
	c := NewControllerWithDeps(&Config{Enabled: true}, pg, nil, nil, nil)
	err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres"})
	if err == nil {
		t.Error("expected error when postgres EnsureEnclave fails, got nil")
	}
}

func TestEnsureEnclaveServices_Idempotent(t *testing.T) {
	pg := newMockPG()
	c := NewControllerWithDeps(&Config{Enabled: true}, pg, nil, nil, nil)

	for i := range 2 {
		if err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres"}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}
	if len(pg.ensureEnclaveCalls) != 2 {
		t.Errorf("EnsureEnclave called %d times, want 2 (idempotent delegate)", len(pg.ensureEnclaveCalls))
	}
}

func TestEnsureEnclaveServices_OnlyRequiredServices(t *testing.T) {
	pg := newMockPG()
	nats := newMockNATS()
	c := NewControllerWithDeps(&Config{Enabled: true}, pg, nats, nil, nil)

	// Only request postgres, not nats
	if err := c.EnsureEnclaveServices(context.Background(), "my-enclave", []string{"postgres"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pg.ensureEnclaveCalls) != 1 {
		t.Errorf("postgres EnsureEnclave called %d times, want 1", len(pg.ensureEnclaveCalls))
	}
	if len(nats.ensureEnclaveCalls) != 0 {
		t.Errorf("nats EnsureEnclave called %d times, want 0 (not requested)", len(nats.ensureEnclaveCalls))
	}
}

// --- ExoServicesFromDeps ---

func TestExoServicesFromDeps_KnownDeps(t *testing.T) {
	deps := map[string]any{
		"tentacular-postgres": struct{}{},
		"tentacular-rustfs":   struct{}{},
		"tentacular-nats":     struct{}{},
		"some-other-dep":      struct{}{},
	}
	svcs := ExoServicesFromDeps(deps)
	if len(svcs) != 3 {
		t.Errorf("ExoServicesFromDeps: got %v, want 3 entries", svcs)
	}
	for _, svc := range svcs {
		switch svc {
		case "postgres", "rustfs", "nats":
			// ok
		default:
			t.Errorf("ExoServicesFromDeps returned unexpected service %q", svc)
		}
	}
}

func TestExoServicesFromDeps_EmptyInput(t *testing.T) {
	svcs := ExoServicesFromDeps(nil)
	if len(svcs) != 0 {
		t.Errorf("ExoServicesFromDeps(nil) = %v, want empty", svcs)
	}
}

// --- DetectExoDepsSlice ---

func TestDetectExoDepsSlice_NoDeps(t *testing.T) {
	deps := DetectExoDepsSlice(nil)
	if len(deps) != 0 {
		t.Errorf("DetectExoDepsSlice(nil) = %v, want empty", deps)
	}
}

func TestDetectExoDepsSlice_WithDeps(t *testing.T) {
	manifests := manifestsWithDeps("tentacular-postgres", "tentacular-rustfs")
	deps := DetectExoDepsSlice(manifests)
	if len(deps) != 2 {
		t.Errorf("DetectExoDepsSlice: got %v, want 2 entries", deps)
	}
}
