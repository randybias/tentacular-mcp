// Package scheduler provides an in-process cron scheduler for tentacular
// workflows. Instead of creating CronJob+Pod+curl for each scheduled trigger,
// the scheduler runs inside the MCP server and triggers workflows via direct
// HTTP to the workflow's ClusterIP service — the same path as wf_run.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// CronAnnotation is the Deployment annotation key that holds the cron schedule.
const CronAnnotation = "tentacular.io/cron-schedule"

// annotationWithFallback returns the value of newKey from annotations,
// falling back to the legacy tentacular.dev/* equivalent if the new key is absent.
// Write paths must always use the new tentacular.io/* key.
func annotationWithFallback(annotations map[string]string, newKey string) string {
	if v := annotations[newKey]; v != "" {
		return v
	}
	if len(newKey) > 14 && newKey[:14] == "tentacular.io/" {
		return annotations["tentacular.dev/"+newKey[14:]]
	}
	return ""
}

// entry tracks a registered workflow schedule.
type entry struct {
	namespace string
	name      string
	schedule  string
	cronID    cron.EntryID
}

// Scheduler manages cron schedules for tentacular workflows.
type Scheduler struct {
	cron    *cron.Cron
	client  *k8s.Client
	logger  *slog.Logger
	entries map[string]entry
	mu      sync.Mutex
}

// New creates a new Scheduler.
func New(client *k8s.Client, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		client:  client,
		logger:  logger,
		entries: make(map[string]entry),
	}
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
	s.logger.Info("cron scheduler started")
}

// Stop gracefully stops the cron scheduler.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("cron scheduler stopped")
}

// Register adds or updates a cron schedule for a workflow.
// If the workflow already has a schedule, it is replaced.
func (s *Scheduler) Register(namespace, name, schedule string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := namespace + "/" + name

	// Remove existing entry if schedule changed
	if existing, ok := s.entries[key]; ok {
		if existing.schedule == schedule {
			return nil // no change
		}
		s.cron.Remove(existing.cronID)
		s.logger.Info("cron schedule updated", "workflow", key, "schedule", schedule)
	} else {
		s.logger.Info("cron schedule registered", "workflow", key, "schedule", schedule)
	}

	id, err := s.cron.AddFunc(schedule, func() {
		s.trigger(namespace, name)
	})
	if err != nil {
		return fmt.Errorf("invalid cron schedule %q for %s: %w", schedule, key, err)
	}

	s.entries[key] = entry{
		cronID:    id,
		namespace: namespace,
		name:      name,
		schedule:  schedule,
	}

	return nil
}

// Deregister removes a workflow's cron schedule.
func (s *Scheduler) Deregister(namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := namespace + "/" + name
	if existing, ok := s.entries[key]; ok {
		s.cron.Remove(existing.cronID)
		delete(s.entries, key)
		s.logger.Info("cron schedule removed", "workflow", key)
	}
}

// Entries returns the number of registered schedules.
func (s *Scheduler) Entries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// trigger fires a workflow run via direct HTTP to the workflow's ClusterIP
// service. Uses a 120s context timeout matching the default wf_run timeout
// to prevent goroutine leaks from hung workflows.
func (s *Scheduler) trigger(namespace, name string) {
	s.logger.Info("cron trigger firing", "workflow", namespace+"/"+name)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	output, err := k8s.RunWorkflow(ctx, s.client, namespace, name, nil)
	if err != nil {
		s.logger.Error("cron trigger failed", "workflow", namespace+"/"+name, "error", err)
		return
	}

	// Log a brief summary of the output
	var result map[string]any
	if json.Unmarshal(output, &result) == nil {
		if success, ok := result["success"].(bool); ok {
			s.logger.Info("cron trigger completed", "workflow", namespace+"/"+name, "success", success)
		} else {
			s.logger.Info("cron trigger completed", "workflow", namespace+"/"+name)
		}
	} else {
		s.logger.Info("cron trigger completed", "workflow", namespace+"/"+name)
	}
}
