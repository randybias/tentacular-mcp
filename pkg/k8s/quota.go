package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QuotaPreset defines resource quota for a namespace.
// Quotas are set on requests (not limits) to allow bursty workloads.
// Most tentacles are event-triggered and idle between runs, so the
// request-based quota reflects actual scheduling demand while pods
// are free to burst to their container limits during execution.
type QuotaPreset struct {
	CPU  string
	Mem  string
	Pods int64
}

var quotaPresets = map[string]QuotaPreset{
	"small":  {CPU: "4", Mem: "4Gi", Pods: 20},
	"medium": {CPU: "16", Mem: "16Gi", Pods: 50},
	"large":  {CPU: "32", Mem: "64Gi", Pods: 100},
}

// CreateResourceQuota creates a ResourceQuota in the given namespace using
// the named preset (small, medium, or large).
func CreateResourceQuota(ctx context.Context, client *Client, namespace, preset string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	p, ok := quotaPresets[preset]
	if !ok {
		return fmt.Errorf("unknown quota preset %q (valid: small, medium, large)", preset)
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tentacular-quota",
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse(p.CPU),
				corev1.ResourceRequestsMemory: resource.MustParse(p.Mem),
				corev1.ResourcePods:           *resource.NewQuantity(p.Pods, resource.DecimalSI),
			},
		},
	}

	_, err := client.Clientset.CoreV1().ResourceQuotas(namespace).Create(ctx, quota, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("resource quota already exists in namespace %q: %w", namespace, err)
		}
		return fmt.Errorf("create resource quota in namespace %q: %w", namespace, err)
	}
	return nil
}

// UpdateResourceQuota updates the tentacular-quota ResourceQuota in the given
// namespace to match the named preset.
func UpdateResourceQuota(ctx context.Context, client *Client, namespace, preset string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	p, ok := quotaPresets[preset]
	if !ok {
		return fmt.Errorf("unknown quota preset %q (valid: small, medium, large)", preset)
	}

	quota, err := client.Clientset.CoreV1().ResourceQuotas(namespace).Get(ctx, "tentacular-quota", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("resource quota not found in namespace %q; use ns_create to initialize", namespace)
		}
		return fmt.Errorf("get resource quota in namespace %q: %w", namespace, err)
	}

	quota.Spec.Hard = corev1.ResourceList{
		corev1.ResourceLimitsCPU:    resource.MustParse(p.CPU),
		corev1.ResourceLimitsMemory: resource.MustParse(p.Mem),
		corev1.ResourcePods:         *resource.NewQuantity(p.Pods, resource.DecimalSI),
	}

	_, err = client.Clientset.CoreV1().ResourceQuotas(namespace).Update(ctx, quota, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update resource quota in namespace %q: %w", namespace, err)
	}
	return nil
}

// CreateLimitRange creates a LimitRange in the given namespace with default
// container resource requests and limits.
func CreateLimitRange(ctx context.Context, client *Client, namespace string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tentacular-limits",
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
		},
	}

	_, err := client.Clientset.CoreV1().LimitRanges(namespace).Create(ctx, lr, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("limit range already exists in namespace %q: %w", namespace, err)
		}
		return fmt.Errorf("create limit range in namespace %q: %w", namespace, err)
	}
	return nil
}
