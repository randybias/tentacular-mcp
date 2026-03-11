package tools

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newExoTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func newExoTestController(cfg *exoskeleton.ExoskeletonConfig) *exoskeleton.ExoskeletonController {
	return exoskeleton.NewExoskeletonController(cfg, nil, nil, nil)
}

// --- exo_status tests ---

func TestExoStatusDisabledNilController(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	result, err := handleExoStatus(ctx, client, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Enabled {
		t.Error("expected Enabled=false with nil controller")
	}
	if len(result.Services) != 0 {
		t.Errorf("expected empty services, got %d", len(result.Services))
	}
}

func TestExoStatusDisabledConfig(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	cfg := &exoskeleton.ExoskeletonConfig{Enabled: false}
	ctrl := newExoTestController(cfg)

	result, err := handleExoStatus(ctx, client, ctrl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Enabled {
		t.Error("expected Enabled=false when config.Enabled=false")
	}
}

func TestExoStatusEnabledAllServices(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	cfg := &exoskeleton.ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		NATSEnabled:     true,
		RustFSEnabled:   false,
		Postgres: exoskeleton.PostgresConfig{
			Host:     "pg.example.com",
			Port:     "5432",
			Database: "tentacular",
		},
		NATS: exoskeleton.NATSConfig{
			URL: "nats://nats.example.com:4222",
		},
	}
	ctrl := newExoTestController(cfg)

	result, err := handleExoStatus(ctx, client, ctrl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Enabled {
		t.Error("expected Enabled=true")
	}
	if len(result.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(result.Services))
	}

	// Postgres
	pg := result.Services[0]
	if pg.Name != "postgres" {
		t.Errorf("expected service[0].Name=postgres, got %q", pg.Name)
	}
	if !pg.Enabled {
		t.Error("expected postgres enabled")
	}
	if !pg.Healthy {
		t.Error("expected postgres healthy")
	}

	// NATS
	nats := result.Services[1]
	if nats.Name != "nats" {
		t.Errorf("expected service[1].Name=nats, got %q", nats.Name)
	}
	if !nats.Enabled {
		t.Error("expected nats enabled")
	}
	if !nats.Healthy {
		t.Error("expected nats healthy")
	}

	// RustFS
	rustfs := result.Services[2]
	if rustfs.Name != "rustfs" {
		t.Errorf("expected service[2].Name=rustfs, got %q", rustfs.Name)
	}
	if rustfs.Enabled {
		t.Error("expected rustfs disabled")
	}
	if rustfs.Healthy {
		t.Error("expected rustfs not healthy when disabled")
	}
}

func TestExoStatusPostgresEnabledNoHost(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	cfg := &exoskeleton.ExoskeletonConfig{
		Enabled:         true,
		PostgresEnabled: true,
		Postgres: exoskeleton.PostgresConfig{
			Host: "", // not configured
		},
	}
	ctrl := newExoTestController(cfg)

	result, err := handleExoStatus(ctx, client, ctrl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pg := result.Services[0]
	if !pg.Enabled {
		t.Error("expected postgres enabled")
	}
	if pg.Healthy {
		t.Error("expected postgres not healthy when host is empty")
	}
	if pg.Detail != "enabled but host not configured" {
		t.Errorf("unexpected detail: %q", pg.Detail)
	}
}

// --- exo_registration tests ---

func TestExoRegistrationNotRegistered(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()
	cfg := &exoskeleton.ExoskeletonConfig{Enabled: true}
	ctrl := newExoTestController(cfg)

	params := ExoRegistrationParams{Namespace: "tent-myapp", Workflow: "myapp"}
	result, err := handleExoRegistration(ctx, client, ctrl, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Registered {
		t.Error("expected Registered=false when no Secret exists")
	}
	if result.Namespace != "tent-myapp" {
		t.Errorf("expected namespace=tent-myapp, got %q", result.Namespace)
	}
	if result.Workflow != "myapp" {
		t.Errorf("expected workflow=myapp, got %q", result.Workflow)
	}
	if result.PostgresRole == "" {
		t.Error("expected PostgresRole to be set from identity compilation")
	}
	if result.NATSSubjectPrefix == "" {
		t.Error("expected NATSSubjectPrefix to be set from identity compilation")
	}
}

func TestExoRegistrationWithSecret(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()
	cfg := &exoskeleton.ExoskeletonConfig{Enabled: true}
	ctrl := newExoTestController(cfg)

	// Create the exoskeleton Secret
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "tentacular-exoskeleton-myapp",
			Namespace:         "tent-myapp",
			CreationTimestamp: metav1.NewTime(ts),
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "myapp",
			},
		},
		Data: map[string][]byte{
			"tentacular-postgres.host": []byte("pg.example.com"),
		},
	}
	_, err := client.Clientset.CoreV1().Secrets("tent-myapp").Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	params := ExoRegistrationParams{Namespace: "tent-myapp", Workflow: "myapp"}
	result, err := handleExoRegistration(ctx, client, ctrl, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Registered {
		t.Error("expected Registered=true when Secret exists")
	}
	if result.SecretName != "tentacular-exoskeleton-myapp" {
		t.Errorf("expected SecretName=tentacular-exoskeleton-myapp, got %q", result.SecretName)
	}
	if result.SecretCreated != "2026-03-10T12:00:00Z" {
		t.Errorf("expected SecretCreated=2026-03-10T12:00:00Z, got %q", result.SecretCreated)
	}
}

func TestExoRegistrationMissingParams(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()
	cfg := &exoskeleton.ExoskeletonConfig{Enabled: true}
	ctrl := newExoTestController(cfg)

	_, err := handleExoRegistration(ctx, client, ctrl, ExoRegistrationParams{Namespace: "", Workflow: "myapp"})
	if err == nil {
		t.Error("expected error when namespace is empty")
	}

	_, err = handleExoRegistration(ctx, client, ctrl, ExoRegistrationParams{Namespace: "ns", Workflow: ""})
	if err == nil {
		t.Error("expected error when workflow is empty")
	}
}

// --- exo_list tests ---

func TestExoListEmpty(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	result, err := handleExoList(ctx, client, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 0 {
		t.Errorf("expected 0 registrations, got %d", len(result.Registrations))
	}
}

func TestExoListMultipleRegistrations(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	// Create multiple exoskeleton Secrets across namespaces
	secrets := []struct {
		ns       string
		workflow string
	}{
		{"tent-alpha", "alpha-wf"},
		{"tent-beta", "beta-wf"},
		{"tent-alpha", "gamma-wf"},
	}

	for _, s := range secrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exoskeleton.ExoskeletonSecretPrefix + s.workflow,
				Namespace: s.ns,
				Labels: map[string]string{
					exoskeleton.ExoskeletonLabel: "true",
					exoskeleton.ReleaseLabel:     s.workflow,
				},
			},
			Data: map[string][]byte{"test": []byte("data")},
		}
		_, err := client.Clientset.CoreV1().Secrets(s.ns).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	result, err := handleExoList(ctx, client, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 3 {
		t.Fatalf("expected 3 registrations, got %d", len(result.Registrations))
	}

	// Verify workflow names are present
	workflows := map[string]bool{}
	for _, r := range result.Registrations {
		workflows[r.Workflow] = true
	}
	for _, s := range secrets {
		if !workflows[s.workflow] {
			t.Errorf("expected workflow %q in list", s.workflow)
		}
	}
}

func TestExoListIgnoresNonExoskeletonSecrets(t *testing.T) {
	client := newExoTestClient()
	ctx := context.Background()

	// Create a regular Secret without the exoskeleton label
	regularSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regular-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{"key": []byte("value")},
	}
	_, err := client.Clientset.CoreV1().Secrets("default").Create(ctx, regularSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Create one exoskeleton Secret
	exoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exoskeleton.ExoskeletonSecretPrefix + "test-wf",
			Namespace: "tent-test",
			Labels: map[string]string{
				exoskeleton.ExoskeletonLabel: "true",
				exoskeleton.ReleaseLabel:     "test-wf",
			},
		},
		Data: map[string][]byte{"test": []byte("data")},
	}
	_, err = client.Clientset.CoreV1().Secrets("tent-test").Create(ctx, exoSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleExoList(ctx, client, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Registrations) != 1 {
		t.Fatalf("expected 1 registration (ignoring non-exoskeleton secret), got %d", len(result.Registrations))
	}
	if result.Registrations[0].Workflow != "test-wf" {
		t.Errorf("expected workflow=test-wf, got %q", result.Registrations[0].Workflow)
	}
}
