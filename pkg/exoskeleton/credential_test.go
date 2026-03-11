package exoskeleton

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCredentialInjectBothServices(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	pgReg := &PostgresRegistration{
		Role:     "tn_myapp_hello",
		Schema:   "tn_myapp_hello",
		Password: "secret123",
		Host:     "pg.example.com",
		Port:     "5432",
		Database: "tentacular",
	}
	natsReg := &NATSRegistration{
		URL:           "nats://nats.example.com:4222",
		Token:         "nats-token-123",
		SubjectPrefix: "tentacular.myapp.hello",
		Principal:     "myapp.hello",
	}

	err := ci.Inject(context.Background(), "tent-myapp", "hello-world", pgReg, natsReg)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	// Verify Secret was created
	secret, err := cs.CoreV1().Secrets("tent-myapp").Get(context.Background(), "tentacular-exoskeleton-hello-world", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	// Verify labels
	if secret.Labels[ReleaseLabel] != "hello-world" {
		t.Errorf("release label: got %s, want hello-world", secret.Labels[ReleaseLabel])
	}
	if secret.Labels[ExoskeletonLabel] != "true" {
		t.Errorf("exoskeleton label: got %s, want true", secret.Labels[ExoskeletonLabel])
	}

	// Verify Postgres keys
	expectedPGKeys := map[string]string{
		"tentacular-postgres.host":     "pg.example.com",
		"tentacular-postgres.port":     "5432",
		"tentacular-postgres.database": "tentacular",
		"tentacular-postgres.user":     "tn_myapp_hello",
		"tentacular-postgres.password": "secret123",
		"tentacular-postgres.schema":   "tn_myapp_hello",
		"tentacular-postgres.protocol": "postgresql",
	}
	for k, want := range expectedPGKeys {
		got := string(secret.Data[k])
		if got != want {
			t.Errorf("key %s: got %q, want %q", k, got, want)
		}
	}

	// Verify NATS keys
	expectedNATSKeys := map[string]string{
		"tentacular-nats.url":      "nats://nats.example.com:4222",
		"tentacular-nats.token":    "nats-token-123",
		"tentacular-nats.protocol": "nats",
	}
	for k, want := range expectedNATSKeys {
		got := string(secret.Data[k])
		if got != want {
			t.Errorf("key %s: got %q, want %q", k, got, want)
		}
	}

	// Verify total key count (7 postgres + 3 nats = 10)
	if len(secret.Data) != 10 {
		t.Errorf("expected 10 data keys, got %d", len(secret.Data))
	}
}

func TestCredentialInjectPostgresOnly(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	pgReg := &PostgresRegistration{
		Role:     "tn_myapp_wf",
		Schema:   "tn_myapp_wf",
		Password: "pw",
		Host:     "pg.local",
		Port:     "5432",
		Database: "db",
	}

	err := ci.Inject(context.Background(), "tent-myapp", "wf", pgReg, nil)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	secret, err := cs.CoreV1().Secrets("tent-myapp").Get(context.Background(), "tentacular-exoskeleton-wf", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	// Should have only Postgres keys (7)
	if len(secret.Data) != 7 {
		t.Errorf("expected 7 data keys (postgres only), got %d", len(secret.Data))
	}

	// NATS keys should not be present
	if _, ok := secret.Data["tentacular-nats.url"]; ok {
		t.Error("NATS url key should not be present when natsReg is nil")
	}
}

func TestCredentialInjectNATSOnly(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	natsReg := &NATSRegistration{
		URL:           "nats://localhost:4222",
		Token:         "tok",
		SubjectPrefix: "tentacular.ns.wf",
		Principal:     "ns.wf",
	}

	err := ci.Inject(context.Background(), "tent-ns", "wf", nil, natsReg)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	secret, err := cs.CoreV1().Secrets("tent-ns").Get(context.Background(), "tentacular-exoskeleton-wf", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	// Should have only NATS keys (3)
	if len(secret.Data) != 3 {
		t.Errorf("expected 3 data keys (nats only), got %d", len(secret.Data))
	}

	// Postgres keys should not be present
	if _, ok := secret.Data["tentacular-postgres.host"]; ok {
		t.Error("Postgres host key should not be present when pgReg is nil")
	}
}

func TestCredentialInjectUpdate(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	// Pre-create a Secret
	_, err := cs.CoreV1().Secrets("tent-myapp").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tentacular-exoskeleton-wf",
			Namespace: "tent-myapp",
			Labels: map[string]string{
				"old-label": "old-value",
			},
		},
		Data: map[string][]byte{
			"old-key": []byte("old-value"),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("pre-create failed: %v", err)
	}

	pgReg := &PostgresRegistration{
		Role: "role", Schema: "schema", Password: "pw",
		Host: "h", Port: "5432", Database: "db",
	}

	err = ci.Inject(context.Background(), "tent-myapp", "wf", pgReg, nil)
	if err != nil {
		t.Fatalf("Inject (update) failed: %v", err)
	}

	secret, err := cs.CoreV1().Secrets("tent-myapp").Get(context.Background(), "tentacular-exoskeleton-wf", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret not found: %v", err)
	}

	// Old key should be gone
	if _, ok := secret.Data["old-key"]; ok {
		t.Error("old data key should be replaced after update")
	}

	// New labels should be set
	if secret.Labels[ExoskeletonLabel] != "true" {
		t.Error("exoskeleton label should be set after update")
	}
	if secret.Labels[ReleaseLabel] != "wf" {
		t.Error("release label should be set after update")
	}

	// Should have postgres keys
	if string(secret.Data["tentacular-postgres.host"]) != "h" {
		t.Errorf("host mismatch: got %q", string(secret.Data["tentacular-postgres.host"]))
	}
}

func TestCredentialRemove(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	// Create a Secret first
	_, err := cs.CoreV1().Secrets("tent-myapp").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tentacular-exoskeleton-wf",
			Namespace: "tent-myapp",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("pre-create failed: %v", err)
	}

	err = ci.Remove(context.Background(), "tent-myapp", "wf")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify Secret is gone
	_, err = cs.CoreV1().Secrets("tent-myapp").Get(context.Background(), "tentacular-exoskeleton-wf", metav1.GetOptions{})
	if err == nil {
		t.Error("Secret should be deleted")
	}
}

func TestCredentialRemoveNotFound(t *testing.T) {
	cs := fake.NewClientset()
	ci := NewCredentialInjector(cs)

	// Remove a non-existent Secret — should not error
	err := ci.Remove(context.Background(), "tent-myapp", "nonexistent")
	if err != nil {
		t.Fatalf("Remove of non-existent Secret should not error: %v", err)
	}
}

func TestCredentialInjectNoClient(t *testing.T) {
	ci := &CredentialInjector{}
	err := ci.Inject(context.Background(), "ns", "wf", nil, nil)
	if err == nil {
		t.Fatal("expected error with nil client")
	}
}

func TestCredentialRemoveNoClient(t *testing.T) {
	ci := &CredentialInjector{}
	err := ci.Remove(context.Background(), "ns", "wf")
	if err == nil {
		t.Fatal("expected error with nil client")
	}
}
