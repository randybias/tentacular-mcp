package scheduler

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ScanWorkflows discovers workflows with cron schedules across all managed
// namespaces and registers them with the scheduler. Call on startup to
// restore schedules after a server restart.
func (s *Scheduler) ScanWorkflows(ctx context.Context) error {
	namespaces, err := k8s.ListManagedNamespaces(ctx, s.client)
	if err != nil {
		return fmt.Errorf("list managed namespaces: %w", err)
	}

	registered := 0
	for _, ns := range namespaces {
		deploys, err := s.client.Clientset.AppsV1().Deployments(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: k8s.ManagedByLabel + "=" + k8s.ManagedByValue,
		})
		if err != nil {
			s.logger.Warn("failed to list deployments", "namespace", ns.Name, "error", err)
			continue
		}

		for _, deploy := range deploys.Items {
			schedule := annotationWithFallback(deploy.Annotations, CronAnnotation)
			if schedule == "" {
				continue
			}

			wfName := deploy.Labels["app.kubernetes.io/name"]
			if wfName == "" {
				wfName = deploy.Name
			}

			if err := s.Register(ns.Name, wfName, schedule); err != nil { //nolint:contextcheck // Register is a cron-scheduler method that does not accept context
				s.logger.Warn("failed to register cron schedule",
					"namespace", ns.Name,
					"workflow", wfName,
					"schedule", schedule,
					"error", err,
				)
				continue
			}
			registered++
		}
	}

	s.logger.Info("cron schedule scan complete", "registered", registered)
	return nil
}
