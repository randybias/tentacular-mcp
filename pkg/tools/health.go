package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// HealthNodesParams are the parameters for health_nodes (empty, cluster-scoped).
type HealthNodesParams struct{}

// NodeConditionInfo is a single node condition.
type NodeConditionInfo struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// HealthNodeInfo is a single node in the health result.
type HealthNodeInfo struct {
	Name           string              `json:"name"`
	CPUCapacity    string              `json:"cpu_capacity"`
	MemCapacity    string              `json:"mem_capacity"`
	CPUAllocatable string              `json:"cpu_allocatable"`
	MemAllocatable string              `json:"mem_allocatable"`
	KubeletVersion string              `json:"kubelet_version"`
	Conditions     []NodeConditionInfo `json:"conditions"`
	Ready          bool                `json:"ready"`
}

// HealthNodesResult is the result of health_nodes.
type HealthNodesResult struct {
	Nodes []HealthNodeInfo `json:"nodes"`
}

// HealthNsUsageParams are the parameters for health_ns_usage.
type HealthNsUsageParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to check resource usage for"`
}

// HealthNsUsageResult is the result of health_ns_usage.
type HealthNsUsageResult struct {
	Namespace string  `json:"namespace"`
	CPUUsed   string  `json:"cpu_used"`
	CPULimit  string  `json:"cpu_limit"`
	MemUsed   string  `json:"mem_used"`
	MemLimit  string  `json:"mem_limit"`
	PodCount  int64   `json:"pod_count"`
	PodLimit  int64   `json:"pod_limit"`
	CPUPct    float64 `json:"cpu_pct"`
	MemPct    float64 `json:"mem_pct"`
	PodPct    float64 `json:"pod_pct"`
}

// HealthClusterSummaryParams are the parameters for health_cluster_summary (empty, cluster-scoped).
type HealthClusterSummaryParams struct{}

// HealthClusterSummaryResult is the result of health_cluster_summary.
type HealthClusterSummaryResult struct {
	CPUCapacity    string `json:"cpu_capacity"`
	CPUAllocatable string `json:"cpu_allocatable"`
	CPURequested   string `json:"cpu_requested"`
	MemCapacity    string `json:"mem_capacity"`
	MemAllocatable string `json:"mem_allocatable"`
	MemRequested   string `json:"mem_requested"`
	TotalNodes     int    `json:"total_nodes"`
	ReadyNodes     int    `json:"ready_nodes"`
	TotalPods      int    `json:"total_pods"`
}

func registerHealthTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "health_nodes",
		Description: "List nodes with readiness, capacity, allocatable resources, kubelet version, and unhealthy conditions.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Node Health",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params HealthNodesParams) (*mcp.CallToolResult, HealthNodesResult, error) {
		result, err := handleHealthNodes(ctx, client)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "health_ns_usage",
		Description: "Compare namespace resource usage against ResourceQuota limits and return utilization percentages.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Namespace Resource Usage",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params HealthNsUsageParams) (*mcp.CallToolResult, HealthNsUsageResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, HealthNsUsageResult{}, err
		}
		result, err := handleHealthNsUsage(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "health_cluster_summary",
		Description: "Aggregate cluster-wide CPU, memory, and pod counts across all nodes.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Cluster Resource Summary",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params HealthClusterSummaryParams) (*mcp.CallToolResult, HealthClusterSummaryResult, error) {
		result, err := handleHealthClusterSummary(ctx, client)
		return nil, result, err
	})
}

func handleHealthNodes(ctx context.Context, client *k8s.Client) (HealthNodesResult, error) {
	nodeList, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return HealthNodesResult{}, fmt.Errorf("list nodes: %w", err)
	}

	nodes := make([]HealthNodeInfo, 0, len(nodeList.Items))
	for _, n := range nodeList.Items {
		ready := false
		conditions := []NodeConditionInfo{}
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				if cond.Status == corev1.ConditionTrue {
					ready = true
				}
			}
			// Collect unhealthy (non-ready) conditions
			if cond.Type != corev1.NodeReady && cond.Status != corev1.ConditionFalse {
				conditions = append(conditions, NodeConditionInfo{
					Type:   string(cond.Type),
					Status: string(cond.Status),
				})
			}
		}

		cpuCap := ""
		memCap := ""
		cpuAlloc := ""
		memAlloc := ""
		if v, ok := n.Status.Capacity[corev1.ResourceCPU]; ok {
			cpuCap = v.String()
		}
		if v, ok := n.Status.Capacity[corev1.ResourceMemory]; ok {
			memCap = v.String()
		}
		if v, ok := n.Status.Allocatable[corev1.ResourceCPU]; ok {
			cpuAlloc = v.String()
		}
		if v, ok := n.Status.Allocatable[corev1.ResourceMemory]; ok {
			memAlloc = v.String()
		}

		nodes = append(nodes, HealthNodeInfo{
			Name:           n.Name,
			Ready:          ready,
			CPUCapacity:    cpuCap,
			MemCapacity:    memCap,
			CPUAllocatable: cpuAlloc,
			MemAllocatable: memAlloc,
			KubeletVersion: n.Status.NodeInfo.KubeletVersion,
			Conditions:     conditions,
		})
	}

	return HealthNodesResult{Nodes: nodes}, nil
}

func handleHealthNsUsage(ctx context.Context, client *k8s.Client, params HealthNsUsageParams) (HealthNsUsageResult, error) {
	quotaList, err := client.Clientset.CoreV1().ResourceQuotas(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return HealthNsUsageResult{}, fmt.Errorf("list resource quotas in namespace %q: %w", params.Namespace, err)
	}
	if len(quotaList.Items) == 0 {
		// No quota set -- report usage without limits
		pods, err := client.Clientset.CoreV1().Pods(params.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return HealthNsUsageResult{}, fmt.Errorf("list pods in namespace %q: %w", params.Namespace, err)
		}
		return HealthNsUsageResult{
			Namespace: params.Namespace,
			CPULimit:  "unlimited",
			MemLimit:  "unlimited",
			PodCount:  int64(len(pods.Items)),
			PodLimit:  -1,
		}, nil
	}

	quota := quotaList.Items[0]

	cpuUsed := quota.Status.Used[corev1.ResourceLimitsCPU]
	cpuLimit := quota.Status.Hard[corev1.ResourceLimitsCPU]
	memUsed := quota.Status.Used[corev1.ResourceLimitsMemory]
	memLimit := quota.Status.Hard[corev1.ResourceLimitsMemory]
	podUsed := quota.Status.Used[corev1.ResourcePods]
	podLimit := quota.Status.Hard[corev1.ResourcePods]

	cpuPct := pctOf(cpuUsed, cpuLimit)
	memPct := pctOf(memUsed, memLimit)

	podUsedVal := podUsed.Value()
	podLimitVal := podLimit.Value()
	podPct := 0.0
	if podLimitVal > 0 {
		podPct = float64(podUsedVal) / float64(podLimitVal) * 100
	}

	return HealthNsUsageResult{
		Namespace: params.Namespace,
		CPUUsed:   cpuUsed.String(),
		CPULimit:  cpuLimit.String(),
		MemUsed:   memUsed.String(),
		MemLimit:  memLimit.String(),
		PodCount:  podUsedVal,
		PodLimit:  podLimitVal,
		CPUPct:    cpuPct,
		MemPct:    memPct,
		PodPct:    podPct,
	}, nil
}

func pctOf(used, limit resource.Quantity) float64 {
	limitVal := limit.MilliValue()
	if limitVal == 0 {
		return 0
	}
	return float64(used.MilliValue()) / float64(limitVal) * 100
}

func handleHealthClusterSummary(ctx context.Context, client *k8s.Client) (HealthClusterSummaryResult, error) {
	nodeList, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return HealthClusterSummaryResult{}, fmt.Errorf("list nodes: %w", err)
	}

	podList, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return HealthClusterSummaryResult{}, fmt.Errorf("list pods: %w", err)
	}

	totalNodes := len(nodeList.Items)
	readyNodes := 0
	totalCPUCap := resource.NewMilliQuantity(0, resource.DecimalSI)
	totalMemCap := resource.NewMilliQuantity(0, resource.BinarySI)
	totalCPUAlloc := resource.NewMilliQuantity(0, resource.DecimalSI)
	totalMemAlloc := resource.NewMilliQuantity(0, resource.BinarySI)

	for _, n := range nodeList.Items {
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
		if v, ok := n.Status.Capacity[corev1.ResourceCPU]; ok {
			totalCPUCap.Add(v)
		}
		if v, ok := n.Status.Capacity[corev1.ResourceMemory]; ok {
			totalMemCap.Add(v)
		}
		if v, ok := n.Status.Allocatable[corev1.ResourceCPU]; ok {
			totalCPUAlloc.Add(v)
		}
		if v, ok := n.Status.Allocatable[corev1.ResourceMemory]; ok {
			totalMemAlloc.Add(v)
		}
	}

	// Aggregate CPU and memory requests from running pods
	totalCPUReq := resource.NewMilliQuantity(0, resource.DecimalSI)
	totalMemReq := resource.NewMilliQuantity(0, resource.BinarySI)
	for _, pod := range podList.Items {
		for _, c := range pod.Spec.Containers {
			if v, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				totalCPUReq.Add(v)
			}
			if v, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				totalMemReq.Add(v)
			}
		}
	}

	return HealthClusterSummaryResult{
		TotalNodes:     totalNodes,
		ReadyNodes:     readyNodes,
		TotalPods:      len(podList.Items),
		CPUCapacity:    totalCPUCap.String(),
		CPUAllocatable: totalCPUAlloc.String(),
		CPURequested:   totalCPUReq.String(),
		MemCapacity:    totalMemCap.String(),
		MemAllocatable: totalMemAlloc.String(),
		MemRequested:   totalMemReq.String(),
	}, nil
}
