package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// GVisorCheckParams are the parameters for gvisor_check (empty, cluster-scoped).
type GVisorCheckParams struct{}

// GVisorCheckResult is the result of gvisor_check.
type GVisorCheckResult struct {
	RuntimeClass string `json:"runtime_class,omitempty"`
	Handler      string `json:"handler,omitempty"`
	Guidance     string `json:"guidance,omitempty"`
	Available    bool   `json:"available"`
}

// GVisorAnnotateNsParams are the parameters for gvisor_annotate_ns.
type GVisorAnnotateNsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to apply gVisor runtime class annotation to"`
}

// GVisorAnnotateNsResult is the result of gvisor_annotate_ns.
type GVisorAnnotateNsResult struct {
	Namespace  string `json:"namespace"`
	Annotation string `json:"annotation"`
	Applied    bool   `json:"applied"`
}

// GVisorVerifyParams are the parameters for gvisor_verify.
type GVisorVerifyParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace in which to create the verification pod"`
}

// GVisorVerifyResult is the result of gvisor_verify.
type GVisorVerifyResult struct {
	Output       string `json:"output"`
	RuntimeClass string `json:"runtime_class"`
	Verified     bool   `json:"verified"`
}

func registerGVisorTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gvisor_check",
		Description: "Check whether a gVisor RuntimeClass is available in the cluster.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Check gVisor Availability",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params GVisorCheckParams) (*mcp.CallToolResult, GVisorCheckResult, error) {
		result, err := handleGVisorCheck(ctx, client)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gvisor_annotate_ns",
		Description: "Annotate a managed namespace with the gVisor runtime class so new pods use gVisor sandboxing.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Annotate Namespace for gVisor",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params GVisorAnnotateNsParams) (*mcp.CallToolResult, GVisorAnnotateNsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, GVisorAnnotateNsResult{}, err
		}
		result, err := handleGVisorAnnotateNs(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gvisor_verify",
		Description: "Verify gVisor sandboxing by creating an ephemeral pod with the gVisor runtime class and checking kernel identity. Creates and deletes an ephemeral verification pod.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Verify gVisor Sandboxing",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params GVisorVerifyParams) (*mcp.CallToolResult, GVisorVerifyResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, GVisorVerifyResult{}, err
		}
		result, err := handleGVisorVerify(ctx, client, params)
		return nil, result, err
	})
}

func handleGVisorCheck(ctx context.Context, client *k8s.Client) (GVisorCheckResult, error) {
	rcs, err := client.Clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return GVisorCheckResult{}, fmt.Errorf("list runtime classes: %w", err)
	}

	for _, rc := range rcs.Items {
		handler := strings.ToLower(rc.Handler)
		if strings.Contains(handler, "gvisor") || strings.Contains(handler, "runsc") {
			return GVisorCheckResult{
				Available:    true,
				RuntimeClass: rc.Name,
				Handler:      rc.Handler,
			}, nil
		}
	}

	return GVisorCheckResult{
		Available: false,
		Guidance:  "No gVisor RuntimeClass found. Install gVisor and create a RuntimeClass with handler 'runsc'.",
	}, nil
}

func handleGVisorAnnotateNs(ctx context.Context, client *k8s.Client, params GVisorAnnotateNsParams) (GVisorAnnotateNsResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return GVisorAnnotateNsResult{}, err
	}

	// Verify gVisor RuntimeClass exists
	checkResult, err := handleGVisorCheck(ctx, client)
	if err != nil {
		return GVisorAnnotateNsResult{}, fmt.Errorf("check gVisor availability: %w", err)
	}
	if !checkResult.Available {
		return GVisorAnnotateNsResult{}, errors.New("gVisor RuntimeClass not found in cluster; install gVisor before applying")
	}

	// Patch namespace annotation
	const annotationKey = "tentacular.io/runtime-class"
	const annotationValue = "gvisor"
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, annotationKey, annotationValue)

	_, err = client.Clientset.CoreV1().Namespaces().Patch(
		ctx,
		params.Namespace,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return GVisorAnnotateNsResult{}, fmt.Errorf("patch namespace %q with gVisor annotation: %w", params.Namespace, err)
	}

	return GVisorAnnotateNsResult{
		Namespace:  params.Namespace,
		Annotation: annotationKey + "=" + annotationValue,
		Applied:    true,
	}, nil
}

func handleGVisorVerify(ctx context.Context, client *k8s.Client, params GVisorVerifyParams) (GVisorVerifyResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return GVisorVerifyResult{}, err
	}
	// Find the gVisor RuntimeClass
	checkResult, err := handleGVisorCheck(ctx, client)
	if err != nil {
		return GVisorVerifyResult{}, fmt.Errorf("check gVisor availability: %w", err)
	}
	if !checkResult.Available {
		return GVisorVerifyResult{}, errors.New("gVisor RuntimeClass not found in cluster")
	}

	runtimeClassName := checkResult.RuntimeClass
	podName := fmt.Sprintf("gvisor-verify-%d", time.Now().UnixNano()%100000)

	trueVal := true
	falseVal := false
	nonRootUID := int64(65534)
	seccompProfile := corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	dropAll := []corev1.Capability{"ALL"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: params.Namespace,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClassName,
			RestartPolicy:    corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &trueVal,
				RunAsUser:      &nonRootUID,
				SeccompProfile: &seccompProfile,
			},
			Containers: []corev1.Container{
				{
					Name:    "verify",
					Image:   "busybox:latest",
					Command: []string{"sh", "-c", "uname -r"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &falseVal,
						ReadOnlyRootFilesystem:   &trueVal,
						RunAsNonRoot:             &trueVal,
						RunAsUser:                &nonRootUID,
						SeccompProfile:           &seccompProfile,
						Capabilities: &corev1.Capabilities{
							Drop: dropAll,
						},
					},
				},
			},
		},
	}

	// Create the pod
	created, err := client.Clientset.CoreV1().Pods(params.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return GVisorVerifyResult{}, fmt.Errorf("create gVisor verification pod: %w", err)
	}

	// Cleanup pod in defer (best-effort)
	defer func() { //nolint:contextcheck // cleanup goroutine intentionally outlives the request context
		delCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Clientset.CoreV1().Pods(params.Namespace).Delete(delCtx, created.Name, metav1.DeleteOptions{})
	}()

	// Wait up to 60 seconds for pod to complete
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		p, getErr := client.Clientset.CoreV1().Pods(params.Namespace).Get(ctx, created.Name, metav1.GetOptions{})
		if getErr != nil {
			return GVisorVerifyResult{}, fmt.Errorf("get verification pod status: %w", getErr)
		}
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Read logs
	logOpts := &corev1.PodLogOptions{Container: "verify"}
	stream, err := client.Clientset.CoreV1().Pods(params.Namespace).GetLogs(created.Name, logOpts).Stream(ctx)
	if err != nil {
		return GVisorVerifyResult{
			Verified:     false,
			Output:       fmt.Sprintf("pod created but could not read logs: %v", err),
			RuntimeClass: runtimeClassName,
		}, nil
	}
	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(stream)
	if err != nil {
		return GVisorVerifyResult{
			Verified:     false,
			Output:       fmt.Sprintf("error reading log stream: %v", err),
			RuntimeClass: runtimeClassName,
		}, nil
	}

	output := strings.TrimSpace(string(data))
	lower := strings.ToLower(output)
	// gVisor's uname -r returns a kernel string containing "gvisor", or the
	// classic emulated version "4.4.0". Check for both patterns.
	verified := strings.Contains(lower, "gvisor") ||
		strings.Contains(lower, "runsc") ||
		strings.HasPrefix(output, "4.4.0")

	return GVisorVerifyResult{
		Verified:     verified,
		Output:       strings.TrimSpace(output),
		RuntimeClass: runtimeClassName,
	}, nil
}
