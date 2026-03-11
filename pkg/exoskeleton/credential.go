package exoskeleton

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// ExoskeletonSecretPrefix is prepended to the workflow name for Secret naming.
	ExoskeletonSecretPrefix = "tentacular-exoskeleton-"

	// ExoskeletonLabel marks Secrets created by the exoskeleton subsystem.
	ExoskeletonLabel = "tentacular.io/exoskeleton"

	// ReleaseLabel identifies the workflow that owns the Secret.
	ReleaseLabel = "tentacular.io/release"
)

// CredentialInjector materializes Kubernetes Secrets with backing-service
// credentials in tentacle namespaces.
type CredentialInjector struct {
	clientset kubernetes.Interface
}

// NewCredentialInjector creates a CredentialInjector with the given K8s clientset.
func NewCredentialInjector(clientset kubernetes.Interface) *CredentialInjector {
	return &CredentialInjector{clientset: clientset}
}

// Inject creates or updates a Secret in the tentacle's namespace containing
// connection details for all registered backing services. Keys follow the
// <dep>.<field> convention (e.g., "tentacular-postgres.host").
func (ci *CredentialInjector) Inject(ctx context.Context, namespace, workflow string, pgReg *PostgresRegistration, natsReg *NATSRegistration) error {
	if ci.clientset == nil {
		return fmt.Errorf("credential injector: no kubernetes client configured")
	}

	secretName := ExoskeletonSecretPrefix + workflow
	data := make(map[string][]byte)

	if pgReg != nil {
		data["tentacular-postgres.host"] = []byte(pgReg.Host)
		data["tentacular-postgres.port"] = []byte(pgReg.Port)
		data["tentacular-postgres.database"] = []byte(pgReg.Database)
		data["tentacular-postgres.user"] = []byte(pgReg.Role)
		data["tentacular-postgres.password"] = []byte(pgReg.Password)
		data["tentacular-postgres.schema"] = []byte(pgReg.Schema)
		data["tentacular-postgres.protocol"] = []byte("postgresql")
	}

	if natsReg != nil {
		data["tentacular-nats.url"] = []byte(natsReg.URL)
		data["tentacular-nats.token"] = []byte(natsReg.Token)
		data["tentacular-nats.protocol"] = []byte("nats")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				ReleaseLabel:     workflow,
				ExoskeletonLabel: "true",
			},
		},
		Data: data,
	}

	secrets := ci.clientset.CoreV1().Secrets(namespace)

	existing, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("credential injector: get existing secret: %w", err)
		}
		// Secret doesn't exist, create it
		if _, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("credential injector: create secret: %w", err)
		}
		return nil
	}

	// Secret exists, update it
	existing.Data = data
	existing.Labels = secret.Labels
	if _, err := secrets.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("credential injector: update secret: %w", err)
	}
	return nil
}

// Remove deletes the exoskeleton Secret for the given workflow.
// Returns nil if the Secret does not exist (idempotent).
func (ci *CredentialInjector) Remove(ctx context.Context, namespace, workflow string) error {
	if ci.clientset == nil {
		return fmt.Errorf("credential injector: no kubernetes client configured")
	}

	secretName := ExoskeletonSecretPrefix + workflow
	err := ci.clientset.CoreV1().Secrets(namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("credential injector: delete secret: %w", err)
	}
	return nil
}
