package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// WfPodsParams are the parameters for wf_pods.
type WfPodsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to list pods in"`
}

// WfPodInfo is a single pod in the list result.
type WfPodInfo struct {
	Name     string   `json:"name"`
	Phase    string   `json:"phase"`
	Age      string   `json:"age"`
	Images   []string `json:"images"`
	Restarts int32    `json:"restarts"`
	Ready    bool     `json:"ready"`
}

// WfPodsResult is the result of wf_pods.
type WfPodsResult struct {
	Pods []WfPodInfo `json:"pods"`
}

// WfLogsParams are the parameters for wf_logs.
type WfLogsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the pod"`
	Pod       string `json:"pod" jsonschema:"Name of the pod to get logs from"`
	Container string `json:"container,omitempty" jsonschema:"Container name (optional, defaults to first container)"`
	TailLines int64  `json:"tail_lines,omitempty" jsonschema:"Number of log lines to return (default 100)"`
}

// WfLogsResult is the result of wf_logs.
type WfLogsResult struct {
	Pod       string   `json:"pod"`
	Container string   `json:"container"`
	Lines     []string `json:"lines"`
}

// WfEventsParams are the parameters for wf_events.
type WfEventsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to list events in"`
	Limit     int64  `json:"limit,omitempty" jsonschema:"Maximum number of events to return (default 100)"`
}

// WfEventInfo is a single event in the list result.
type WfEventInfo struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Object   string `json:"object"`
	LastSeen string `json:"last_seen"`
	Count    int32  `json:"count"`
}

// WfEventsResult is the result of wf_events.
type WfEventsResult struct {
	Events []WfEventInfo `json:"events"`
}

// WfJobsParams are the parameters for wf_jobs.
type WfJobsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to list jobs in"`
}

// WfJobInfo is a single job in the list result.
type WfJobInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Start      string `json:"start,omitempty"`
	Completion string `json:"completion,omitempty"`
	Duration   string `json:"duration,omitempty"`
}

// WfCronJobInfo is a single cronjob in the list result.
type WfCronJobInfo struct {
	Name          string `json:"name"`
	Schedule      string `json:"schedule"`
	LastScheduled string `json:"last_scheduled,omitempty"`
	Active        int    `json:"active"`
	Suspended     bool   `json:"suspended"`
}

// WfJobsResult is the result of wf_jobs.
type WfJobsResult struct {
	Jobs     []WfJobInfo     `json:"jobs"`
	CronJobs []WfCronJobInfo `json:"cronjobs"`
}

// WfRestartParams are the parameters for wf_restart.
type WfRestartParams struct {
	Namespace  string `json:"namespace" jsonschema:"Namespace containing the deployment"`
	Deployment string `json:"deployment" jsonschema:"Name of the deployment to restart"`
}

// WfRestartResult is the result of wf_restart.
type WfRestartResult struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Restarted  bool   `json:"restarted"`
}

func registerWorkflowTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_pods",
		Description: "List pods in a namespace with phase, readiness, restart count, images, and age.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Workflow Pods",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfPodsParams) (*mcp.CallToolResult, WfPodsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfPodsResult{}, err
		}
		result, err := handleWfPods(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_logs",
		Description: "Get pod logs from a namespace. Returns tail lines (default 100).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Pod Logs",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfLogsParams) (*mcp.CallToolResult, WfLogsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfLogsResult{}, err
		}
		if err := guard.CheckName(params.Pod); err != nil {
			return nil, WfLogsResult{}, err
		}
		result, err := handleWfLogs(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_events",
		Description: "List events in a namespace sorted by most recent first.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Namespace Events",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfEventsParams) (*mcp.CallToolResult, WfEventsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfEventsResult{}, err
		}
		result, err := handleWfEvents(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_jobs",
		Description: "List Jobs and CronJobs in a namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Jobs and CronJobs",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfJobsParams) (*mcp.CallToolResult, WfJobsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfJobsResult{}, err
		}
		result, err := handleWfJobs(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_restart",
		Description: "Rollout restart a deployment in a managed namespace by patching the pod template with a restart timestamp. Useful after ConfigMap/Secret changes, credential rotation, or gVisor enablement.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Restart Workflow Deployment",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfRestartParams) (*mcp.CallToolResult, WfRestartResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfRestartResult{}, err
		}
		if err := guard.CheckName(params.Deployment); err != nil {
			return nil, WfRestartResult{}, err
		}
		result, err := handleWfRestart(ctx, client, params)
		return nil, result, err
	})
}

func handleWfPods(ctx context.Context, client *k8s.Client, params WfPodsParams) (WfPodsResult, error) {
	podList, err := client.Clientset.CoreV1().Pods(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return WfPodsResult{}, fmt.Errorf("list pods in namespace %q: %w", params.Namespace, err)
	}

	pods := make([]WfPodInfo, 0, len(podList.Items))
	for _, pod := range podList.Items {
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}

		var restarts int32
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}

		images := make([]string, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			images = append(images, c.Image)
		}

		age := time.Since(pod.CreationTimestamp.Time).Round(time.Second).String()

		pods = append(pods, WfPodInfo{
			Name:     pod.Name,
			Phase:    string(pod.Status.Phase),
			Ready:    ready,
			Restarts: restarts,
			Images:   images,
			Age:      age,
		})
	}

	return WfPodsResult{Pods: pods}, nil
}

func handleWfLogs(ctx context.Context, client *k8s.Client, params WfLogsParams) (WfLogsResult, error) {
	const maxTailLines = 10_000
	tailLines := params.TailLines
	if tailLines <= 0 {
		tailLines = 100
	} else if tailLines > maxTailLines {
		tailLines = maxTailLines
	}

	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}
	if params.Container != "" {
		opts.Container = params.Container
	}

	req := client.Clientset.CoreV1().Pods(params.Namespace).GetLogs(params.Pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return WfLogsResult{}, fmt.Errorf("get logs for pod %q in namespace %q: %w", params.Pod, params.Namespace, err)
	}
	defer func() { _ = stream.Close() }()

	const maxLogBytes = 1 << 20 // 1MiB
	data, err := io.ReadAll(io.LimitReader(stream, maxLogBytes))
	if err != nil {
		return WfLogsResult{}, fmt.Errorf("read logs for pod %q: %w", params.Pod, err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}

	container := params.Container
	if container == "" {
		// Determine the actual container name from the pod spec
		pod, err := client.Clientset.CoreV1().Pods(params.Namespace).Get(ctx, params.Pod, metav1.GetOptions{})
		if err == nil && len(pod.Spec.Containers) > 0 {
			container = pod.Spec.Containers[0].Name
		}
	}

	return WfLogsResult{
		Pod:       params.Pod,
		Container: container,
		Lines:     lines,
	}, nil
}

func handleWfEvents(ctx context.Context, client *k8s.Client, params WfEventsParams) (WfEventsResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}

	eventList, err := client.Clientset.CoreV1().Events(params.Namespace).List(ctx, metav1.ListOptions{
		Limit: limit,
	})
	if err != nil {
		return WfEventsResult{}, fmt.Errorf("list events in namespace %q: %w", params.Namespace, err)
	}

	events := make([]WfEventInfo, 0, len(eventList.Items))
	for _, e := range eventList.Items {
		lastSeen := ""
		if !e.LastTimestamp.IsZero() {
			lastSeen = e.LastTimestamp.Format(time.RFC3339)
		}
		object := fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name)
		events = append(events, WfEventInfo{
			Type:     e.Type,
			Reason:   e.Reason,
			Message:  e.Message,
			Object:   object,
			Count:    e.Count,
			LastSeen: lastSeen,
		})
	}

	// Sort by last seen descending (most recent first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].LastSeen > events[j].LastSeen
	})

	return WfEventsResult{Events: events}, nil
}

func handleWfJobs(ctx context.Context, client *k8s.Client, params WfJobsParams) (WfJobsResult, error) {
	jobList, err := client.Clientset.BatchV1().Jobs(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return WfJobsResult{}, fmt.Errorf("list jobs in namespace %q: %w", params.Namespace, err)
	}

	cronJobList, err := client.Clientset.BatchV1().CronJobs(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return WfJobsResult{}, fmt.Errorf("list cronjobs in namespace %q: %w", params.Namespace, err)
	}

	jobs := make([]WfJobInfo, 0, len(jobList.Items))
	for _, job := range jobList.Items {
		status := jobStatus(job)
		start := ""
		completion := ""
		duration := ""
		if job.Status.StartTime != nil {
			start = job.Status.StartTime.Format(time.RFC3339)
		}
		if job.Status.CompletionTime != nil {
			completion = job.Status.CompletionTime.Format(time.RFC3339)
			if job.Status.StartTime != nil {
				d := job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
				duration = d.Round(time.Second).String()
			}
		}
		jobs = append(jobs, WfJobInfo{
			Name:       job.Name,
			Status:     status,
			Start:      start,
			Completion: completion,
			Duration:   duration,
		})
	}

	cronJobs := make([]WfCronJobInfo, 0, len(cronJobList.Items))
	for _, cj := range cronJobList.Items {
		lastScheduled := ""
		if cj.Status.LastScheduleTime != nil {
			lastScheduled = cj.Status.LastScheduleTime.Format(time.RFC3339)
		}
		suspended := false
		if cj.Spec.Suspend != nil {
			suspended = *cj.Spec.Suspend
		}
		cronJobs = append(cronJobs, WfCronJobInfo{
			Name:          cj.Name,
			Schedule:      cj.Spec.Schedule,
			LastScheduled: lastScheduled,
			Active:        len(cj.Status.Active),
			Suspended:     suspended,
		})
	}

	return WfJobsResult{Jobs: jobs, CronJobs: cronJobs}, nil
}

func jobStatus(job batchv1.Job) string {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			return "Complete"
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return "Failed"
		}
	}
	if job.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

func handleWfRestart(ctx context.Context, client *k8s.Client, params WfRestartParams) (WfRestartResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WfRestartResult{}, err
	}

	// Verify the deployment exists.
	_, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Deployment, metav1.GetOptions{})
	if err != nil {
		return WfRestartResult{}, fmt.Errorf("get deployment %q in namespace %q: %w", params.Deployment, params.Namespace, err)
	}

	// Rollout restart: patch the pod template annotation with a timestamp.
	// This is the same mechanism as `kubectl rollout restart`.
	restartAnnotation := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"tentacular.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}

	patchBody, err := json.Marshal(restartAnnotation)
	if err != nil {
		return WfRestartResult{}, fmt.Errorf("marshal restart patch: %w", err)
	}

	_, err = client.Clientset.AppsV1().Deployments(params.Namespace).Patch(
		ctx, params.Deployment, types.MergePatchType, patchBody, metav1.PatchOptions{},
	)
	if err != nil {
		return WfRestartResult{}, fmt.Errorf("patch deployment %q for rollout restart: %w", params.Deployment, err)
	}

	return WfRestartResult{
		Namespace:  params.Namespace,
		Deployment: params.Deployment,
		Restarted:  true,
	}, nil
}
