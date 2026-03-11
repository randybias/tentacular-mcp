package proxy

import (
	"context"
	"log/slog"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

const (
	// DefaultInterval is how often the reconciler checks proxy health.
	DefaultInterval = 5 * time.Minute
)

// Reconciler ensures the esm.sh module proxy Deployment and Service are
// running in the configured namespace. It runs as a background goroutine
// from MCP server startup.
type Reconciler struct {
	client   *k8s.Client
	opts     Options
	interval time.Duration
	logger   *slog.Logger
}

// NewReconciler creates a new proxy Reconciler.
func NewReconciler(client *k8s.Client, opts Options, logger *slog.Logger) *Reconciler {
	if opts.Namespace == "" {
		opts.Namespace = DefaultNamespace
	}
	interval := DefaultInterval
	return &Reconciler{
		client:   client,
		opts:     opts,
		interval: interval,
		logger:   logger,
	}
}

// Run starts the reconciliation loop and blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	r.logger.Info("proxy reconciler starting", "namespace", r.opts.Namespace, "image", r.opts.image(), "interval", r.interval)

	// Reconcile immediately on startup
	r.reconcileOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("proxy reconciler stopped")
			return
		case <-ticker.C:
			r.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce checks proxy state and creates/updates resources as needed.
func (r *Reconciler) reconcileOnce(ctx context.Context) {
	if err := r.reconcileDeployment(ctx); err != nil {
		r.logger.Error("proxy reconcile deployment failed", "error", err)
		return
	}
	if err := r.reconcileService(ctx); err != nil {
		r.logger.Error("proxy reconcile service failed", "error", err)
	}
}

func (r *Reconciler) reconcileDeployment(ctx context.Context) error {
	desired := BuildDeployment(r.opts)
	ns := r.opts.Namespace

	existing, err := r.client.Clientset.AppsV1().Deployments(ns).Get(ctx, DeploymentName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := r.client.Clientset.AppsV1().Deployments(ns).Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		r.logger.Info("proxy deployment created", "namespace", ns)
		return nil
	}
	if err != nil {
		return err
	}

	// Update image if it has changed
	if len(existing.Spec.Template.Spec.Containers) > 0 &&
		existing.Spec.Template.Spec.Containers[0].Image != r.opts.image() {
		desired.ResourceVersion = existing.ResourceVersion
		_, updateErr := r.client.Clientset.AppsV1().Deployments(ns).Update(ctx, desired, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}
		r.logger.Info("proxy deployment updated", "namespace", ns, "image", r.opts.image())
	}
	return nil
}

func (r *Reconciler) reconcileService(ctx context.Context) error {
	desired := BuildService(r.opts)
	ns := r.opts.Namespace

	_, err := r.client.Clientset.CoreV1().Services(ns).Get(ctx, ServiceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := r.client.Clientset.CoreV1().Services(ns).Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		r.logger.Info("proxy service created", "namespace", ns)
		return nil
	}
	return err
}

// Status returns the current installation and readiness state of the proxy.
type Status struct {
	Installed bool
	Ready     bool
	Image     string
	Storage   string
}

// Namespace returns the namespace used for status checks.
// When the umbrella chart sets StatusNamespace, that is returned instead.
func (r *Reconciler) Namespace() string {
	return r.opts.statusNamespace()
}

// GetStatus returns the current proxy status from the K8s API.
// In chart-managed mode, StatusDeploymentName and StatusNamespace override
// the default deployment name and namespace so the correct deployment is found.
func (r *Reconciler) GetStatus(ctx context.Context) Status {
	dep, err := r.client.Clientset.AppsV1().Deployments(r.opts.statusNamespace()).Get(ctx, r.opts.statusDeploymentName(), metav1.GetOptions{})
	if err != nil {
		return Status{Installed: false}
	}

	image := r.opts.image()
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		image = dep.Spec.Template.Spec.Containers[0].Image
	}

	return Status{
		Installed: true,
		Ready:     dep.Status.ReadyReplicas >= 1,
		Image:     image,
		Storage:   r.opts.storageType(),
	}
}
