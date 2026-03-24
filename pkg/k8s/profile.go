package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ExoskeletonServiceInfo describes a single exoskeleton backing service.
type ExoskeletonServiceInfo struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          string `json:"port"`
	Protocol      string `json:"protocol"`
	Available     bool   `json:"available"`
	SPIFFEEnabled bool   `json:"spiffeEnabled,omitempty"`
}

// ExoskeletonAuthInfo describes the OIDC authentication status.
type ExoskeletonAuthInfo struct {
	Enabled bool   `json:"enabled"`
	Issuer  string `json:"issuer,omitempty"`
}

// ExoskeletonInfo describes the exoskeleton subsystem and its services.
type ExoskeletonInfo struct {
	Enabled  bool                     `json:"enabled"`
	Services []ExoskeletonServiceInfo `json:"services"`
	Auth     ExoskeletonAuthInfo      `json:"auth"`
}

// ClusterProfile contains a point-in-time capability snapshot of the cluster.
type ClusterProfile struct {
	GeneratedAt    time.Time          `json:"generatedAt"`
	LimitRange     *LimitRangeSummary `json:"limitRange,omitempty"`
	Quota          *QuotaSummary      `json:"quota,omitempty"`
	K8sVersion     string             `json:"k8sVersion"`
	Distribution   string             `json:"distribution"`
	PodSecurity    string             `json:"podSecurity"`
	Namespace      string             `json:"namespace"`
	RWXNote        string             `json:"rwxNote"`
	CNI            CNIInfo            `json:"cni"`
	RuntimeClasses []RuntimeClassInfo `json:"runtimeClasses"`
	Ingress        []string           `json:"ingress"`
	CSIDrivers     []string           `json:"csiDrivers"`
	StorageClasses []StorageClassInfo `json:"storageClasses"`
	Nodes          []NodeInfo         `json:"nodes"`
	Guidance       []string           `json:"guidance"`
	Warnings       []string           `json:"warnings,omitempty"`
	Extensions     ExtensionSet       `json:"extensions"`
	NetworkPolicy  NetPolInfo         `json:"networkPolicy"`
	Exoskeleton    *ExoskeletonInfo   `json:"exoskeleton,omitempty"`
	GVisor         bool               `json:"gvisor"`
}

// NodeInfo describes a single cluster node.
type NodeInfo struct {
	Labels           map[string]string `json:"labels"`
	Allocatable      map[string]string `json:"allocatable"`
	Capacity         map[string]string `json:"capacity"`
	Name             string            `json:"name"`
	OS               string            `json:"os"`
	Arch             string            `json:"arch"`
	KubeletVersion   string            `json:"kubeletVersion"`
	KernelVersion    string            `json:"kernelVersion"`
	ContainerRuntime string            `json:"containerRuntime"`
	Taints           []string          `json:"taints"`
	Ready            bool              `json:"ready"`
}

// RuntimeClassInfo describes a RuntimeClass.
type RuntimeClassInfo struct {
	Name    string `json:"name"`
	Handler string `json:"handler"`
}

// CNIInfo describes the detected CNI plugin.
type CNIInfo struct {
	Name                   string `json:"name"`
	Version                string `json:"version,omitempty"`
	NetworkPolicySupported bool   `json:"networkPolicySupported"`
	EgressSupported        bool   `json:"egressSupported"`
}

// NetPolInfo describes NetworkPolicy support and usage.
type NetPolInfo struct {
	Supported bool `json:"supported"`
	InUse     bool `json:"inUse"`
}

// StorageClassInfo describes a StorageClass.
// RWXCapable is inferred from the provisioner name — it is a heuristic hint,
// not a guarantee. See ClusterProfile.RWXNote for the qualification.
type StorageClassInfo struct {
	Name                 string `json:"name"`
	Provisioner          string `json:"provisioner"`
	ReclaimPolicy        string `json:"reclaimPolicy"`
	IsDefault            bool   `json:"isDefault"`
	AllowVolumeExpansion bool   `json:"allowVolumeExpansion"`
	RWXCapable           bool   `json:"rwxCapable"`
}

// ExtensionSet records which well-known CRD-based extensions are installed.
type ExtensionSet struct {
	OtherCRDGroups  []string `json:"otherCRDGroups"`
	Istio           bool     `json:"istio"`
	CertManager     bool     `json:"certManager"`
	PrometheusOp    bool     `json:"prometheusOp"`
	ExternalSecrets bool     `json:"externalSecrets"`
	ArgoCD          bool     `json:"argoCD"`
	GatewayAPI      bool     `json:"gatewayAPI"`
	MetricsServer   bool     `json:"metricsServer"`
}

// QuotaSummary is a simplified view of a ResourceQuota.
type QuotaSummary struct {
	CPURequest    string `json:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
	MaxPods       int    `json:"maxPods,omitempty"`
}

// LimitRangeSummary is a simplified view of a LimitRange.
type LimitRangeSummary struct {
	DefaultCPURequest    string `json:"defaultCPURequest,omitempty"`
	DefaultCPULimit      string `json:"defaultCPULimit,omitempty"`
	DefaultMemoryRequest string `json:"defaultMemoryRequest,omitempty"`
	DefaultMemoryLimit   string `json:"defaultMemoryLimit,omitempty"`
}

// ProfileCluster performs a full scan of the cluster and returns a ClusterProfile.
func ProfileCluster(ctx context.Context, client *Client, namespace string) (*ClusterProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout*2)
	defer cancel()

	profile := &ClusterProfile{
		GeneratedAt:    time.Now().UTC(),
		Namespace:      namespace,
		Nodes:          []NodeInfo{},
		RuntimeClasses: []RuntimeClassInfo{},
		StorageClasses: []StorageClassInfo{},
		CSIDrivers:     []string{},
		Ingress:        []string{},
	}

	if err := profileVersion(ctx, client, profile); err != nil {
		return nil, err
	}
	if err := profileNodes(ctx, client, profile); err != nil {
		return nil, err
	}
	if err := profileRuntimeClasses(ctx, client, profile); err != nil {
		return nil, err
	}
	if err := profileCNI(ctx, client, profile); err != nil {
		return nil, err
	}
	profileNetworkPolicy(ctx, client, profile)
	if err := profileStorageClasses(ctx, client, profile); err != nil {
		return nil, err
	}
	if err := profileCSIDrivers(ctx, client, profile); err != nil {
		return nil, err
	}
	profileIngress(ctx, client, profile)
	profileExtensions(ctx, client, profile)
	if err := profileNamespaceDetails(ctx, client, profile, namespace); err != nil {
		return nil, err
	}

	profile.Guidance = deriveGuidance(profile)

	return profile, nil
}

func profileVersion(_ context.Context, client *Client, profile *ClusterProfile) error {
	info, err := client.Clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("get server version: %w", err)
	}
	profile.K8sVersion = info.GitVersion
	return nil
}

func profileNodes(ctx context.Context, client *Client, profile *ClusterProfile) error {
	nodes, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	for _, n := range nodes.Items {
		ready := false
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}

		alloc := make(map[string]string)
		for k, v := range n.Status.Allocatable {
			alloc[string(k)] = v.String()
		}
		capacity := make(map[string]string)
		for k, v := range n.Status.Capacity {
			capacity[string(k)] = v.String()
		}

		var taints []string
		for _, t := range n.Spec.Taints {
			taints = append(taints, fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect))
		}

		profile.Nodes = append(profile.Nodes, NodeInfo{
			Name:             n.Name,
			Ready:            ready,
			OS:               n.Status.NodeInfo.OperatingSystem,
			Arch:             n.Status.NodeInfo.Architecture,
			KubeletVersion:   n.Status.NodeInfo.KubeletVersion,
			KernelVersion:    n.Status.NodeInfo.KernelVersion,
			ContainerRuntime: n.Status.NodeInfo.ContainerRuntimeVersion,
			Labels:           n.Labels,
			Taints:           taints,
			Allocatable:      alloc,
			Capacity:         capacity,
		})
	}

	profile.Distribution = detectDistribution(nodes.Items)
	return nil
}

func detectDistribution(nodes []corev1.Node) string {
	for _, n := range nodes {
		labels := n.Labels
		if _, ok := labels["eks.amazonaws.com/nodegroup"]; ok {
			return "eks"
		}
		if _, ok := labels["cloud.google.com/gke-nodepool"]; ok {
			return "gke"
		}
		if _, ok := labels["kubernetes.azure.com/agentpool"]; ok {
			return "aks"
		}
		if _, ok := labels["node.k0sproject.io/role"]; ok {
			return "k0s"
		}
		if it, ok := labels["node.kubernetes.io/instance-type"]; ok && strings.Contains(strings.ToLower(it), "k3s") {
			return "k3s"
		}
	}
	return "vanilla"
}

func profileRuntimeClasses(ctx context.Context, client *Client, profile *ClusterProfile) error {
	rcs, err := client.Clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list runtime classes: %w", err)
	}

	for _, rc := range rcs.Items {
		profile.RuntimeClasses = append(profile.RuntimeClasses, RuntimeClassInfo{
			Name:    rc.Name,
			Handler: rc.Handler,
		})
		if rc.Name == "gvisor" || strings.Contains(rc.Handler, "runsc") {
			profile.GVisor = true
		}
	}
	return nil
}

func profileCNI(ctx context.Context, client *Client, profile *ClusterProfile) error {
	pods, err := client.Clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list kube-system pods: %w", err)
	}

	for _, pod := range pods.Items {
		app := pod.Labels["k8s-app"]
		name := pod.Name
		switch {
		case app == "calico-node":
			profile.CNI = CNIInfo{Name: "calico", NetworkPolicySupported: true, EgressSupported: true}
			return nil
		case app == "cilium":
			profile.CNI = CNIInfo{Name: "cilium", NetworkPolicySupported: true, EgressSupported: true}
			return nil
		case app == "kube-router":
			profile.CNI = CNIInfo{Name: "kube-router", NetworkPolicySupported: true, EgressSupported: true}
			return nil
		case strings.Contains(name, "weave"):
			profile.CNI = CNIInfo{Name: "weave", NetworkPolicySupported: true, EgressSupported: true}
			return nil
		case strings.Contains(name, "flannel"):
			profile.CNI = CNIInfo{Name: "flannel", NetworkPolicySupported: false, EgressSupported: false}
			return nil
		case strings.Contains(name, "kindnet"):
			profile.CNI = CNIInfo{Name: "kindnet", NetworkPolicySupported: false, EgressSupported: false}
			return nil
		}
	}

	profile.CNI = CNIInfo{Name: "unknown", NetworkPolicySupported: false, EgressSupported: false}
	return nil
}

func profileNetworkPolicy(ctx context.Context, client *Client, profile *ClusterProfile) {
	netpols, err := client.Clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err == nil {
		profile.NetworkPolicy = NetPolInfo{
			Supported: profile.CNI.NetworkPolicySupported,
			InUse:     len(netpols.Items) > 0,
		}
	} else {
		slog.Warn("network policy detection degraded", "error", err)
		profile.NetworkPolicy = NetPolInfo{Supported: profile.CNI.NetworkPolicySupported}
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("network policy inspection failed: %v — inUse may be inaccurate", err))
	}
}

func profileStorageClasses(ctx context.Context, client *Client, profile *ClusterProfile) error {
	scs, err := client.Clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list storage classes: %w", err)
	}

	for _, sc := range scs.Items {
		isDefault := false
		if sc.Annotations != nil {
			isDefault = sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true"
		}

		reclaimPolicy := "Delete"
		if sc.ReclaimPolicy != nil {
			reclaimPolicy = string(*sc.ReclaimPolicy)
		}

		allowExpand := false
		if sc.AllowVolumeExpansion != nil {
			allowExpand = *sc.AllowVolumeExpansion
		}

		profile.StorageClasses = append(profile.StorageClasses, StorageClassInfo{
			Name:                 sc.Name,
			Provisioner:          sc.Provisioner,
			IsDefault:            isDefault,
			ReclaimPolicy:        reclaimPolicy,
			AllowVolumeExpansion: allowExpand,
			RWXCapable:           isRWXCapable(sc.Provisioner),
		})
	}

	if len(profile.StorageClasses) > 0 {
		profile.RWXNote = "rwxCapable is inferred from provisioner name only — not verified. Actual RWX support depends on CSI driver version and cluster configuration."
	}

	return nil
}

func isRWXCapable(provisioner string) bool {
	rwxKeywords := []string{"efs", "nfs", "azurefile", "azure-file", "cephfs", "glusterfs", "rbd"}
	lower := strings.ToLower(provisioner)
	for _, kw := range rwxKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func profileCSIDrivers(ctx context.Context, client *Client, profile *ClusterProfile) error {
	drivers, err := client.Clientset.StorageV1().CSIDrivers().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list CSI drivers: %w", err)
	}

	for _, d := range drivers.Items {
		profile.CSIDrivers = append(profile.CSIDrivers, d.Name)
	}
	return nil
}

func profileIngress(ctx context.Context, client *Client, profile *ClusterProfile) {
	allPods, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		// Non-fatal: ingress detection is best-effort.
		slog.Warn("ingress detection skipped", "error", err)
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("ingress detection skipped: %v — ingress list may be incomplete", err))
		return
	}

	seen := map[string]bool{}
	for _, pod := range allPods.Items {
		labels := pod.Labels
		name := pod.Name
		appName := labels["app.kubernetes.io/name"]
		app := labels["app"]
		switch {
		case appName == "ingress-nginx" || strings.Contains(name, "ingress-nginx"):
			seen["nginx"] = true
		case appName == "traefik" || app == "traefik":
			seen["traefik"] = true
		case app == "istio-ingressgateway" || strings.Contains(name, "istio-ingressgateway"):
			seen["istio"] = true
		case appName == "contour" || app == "contour":
			seen["contour"] = true
		}
	}

	for k := range seen {
		profile.Ingress = append(profile.Ingress, k)
	}
	sort.Strings(profile.Ingress)
}

func profileExtensions(ctx context.Context, client *Client, profile *ClusterProfile) {
	if client.Dynamic == nil {
		return
	}

	crdGVR := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
	crdList, err := client.Dynamic.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		// Non-fatal: extension detection is best-effort.
		slog.Warn("extension detection skipped", "error", err)
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("extension detection skipped: %v — extensions may be incomplete", err))
		return
	}

	profile.Extensions = classifyExtensions(crdList)

	// Metrics server (kube-system label selector)
	msPods, err := client.Clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "k8s-app=metrics-server",
	})
	if err == nil && len(msPods.Items) > 0 {
		profile.Extensions.MetricsServer = true
	}
}

func classifyExtensions(crdList *unstructured.UnstructuredList) ExtensionSet {
	ext := ExtensionSet{}
	knownGroups := map[string]bool{}

	for _, item := range crdList.Items {
		name := item.GetName()
		parts := strings.SplitN(name, ".", 2)
		if len(parts) < 2 {
			continue
		}
		group := parts[1]

		switch {
		case strings.HasSuffix(group, ".istio.io") || group == "networking.istio.io" || group == "security.istio.io":
			ext.Istio = true
		case group == "cert-manager.io" || strings.HasSuffix(group, ".cert-manager.io"):
			ext.CertManager = true
		case group == "monitoring.coreos.com":
			ext.PrometheusOp = true
		case group == "external-secrets.io" || strings.HasSuffix(group, ".external-secrets.io"):
			ext.ExternalSecrets = true
		case group == "argoproj.io":
			ext.ArgoCD = true
		case group == "gateway.networking.k8s.io":
			ext.GatewayAPI = true
		default:
			if !knownGroups[group] {
				knownGroups[group] = true
				ext.OtherCRDGroups = append(ext.OtherCRDGroups, group)
			}
		}
	}
	sort.Strings(ext.OtherCRDGroups)
	const maxOtherCRDGroups = 20
	if len(ext.OtherCRDGroups) > maxOtherCRDGroups {
		ext.OtherCRDGroups = append(
			ext.OtherCRDGroups[:maxOtherCRDGroups],
			fmt.Sprintf("... and %d more (truncated)", len(ext.OtherCRDGroups)-maxOtherCRDGroups),
		)
	}
	return ext
}

func profileNamespaceDetails(ctx context.Context, client *Client, profile *ClusterProfile, namespace string) error {
	if namespace == "" {
		return nil
	}

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get namespace %q: %w", namespace, err)
	}

	if ns.Labels != nil {
		profile.PodSecurity = ns.Labels["pod-security.kubernetes.io/enforce"]
	}

	quotas, err := client.Clientset.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err == nil && len(quotas.Items) > 0 {
		qs := &QuotaSummary{}
		for _, q := range quotas.Items {
			hard := q.Spec.Hard
			if v, ok := hard[corev1.ResourceRequestsCPU]; ok {
				qs.CPURequest = v.String()
			}
			if v, ok := hard[corev1.ResourceLimitsCPU]; ok {
				qs.CPULimit = v.String()
			}
			if v, ok := hard[corev1.ResourceRequestsMemory]; ok {
				qs.MemoryRequest = v.String()
			}
			if v, ok := hard[corev1.ResourceLimitsMemory]; ok {
				qs.MemoryLimit = v.String()
			}
			if v, ok := hard[corev1.ResourcePods]; ok {
				pods, _ := v.AsInt64()
				qs.MaxPods = int(pods)
			}
		}
		profile.Quota = qs
	}

	lrs, err := client.Clientset.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if err == nil && len(lrs.Items) > 0 {
		lr := &LimitRangeSummary{}
		for _, r := range lrs.Items {
			for _, item := range r.Spec.Limits {
				if item.Type == corev1.LimitTypeContainer {
					if v, ok := item.Default[corev1.ResourceCPU]; ok {
						lr.DefaultCPULimit = v.String()
					}
					if v, ok := item.DefaultRequest[corev1.ResourceCPU]; ok {
						lr.DefaultCPURequest = v.String()
					}
					if v, ok := item.Default[corev1.ResourceMemory]; ok {
						lr.DefaultMemoryLimit = v.String()
					}
					if v, ok := item.DefaultRequest[corev1.ResourceMemory]; ok {
						lr.DefaultMemoryRequest = v.String()
					}
				}
			}
		}
		profile.LimitRange = lr
	}

	return nil
}

func deriveGuidance(p *ClusterProfile) []string {
	var g []string

	if p.GVisor {
		g = append(g, "Use runtime_class: gvisor for untrusted workflow steps")
	} else {
		g = append(g, "gVisor not available — omit runtime_class or set it to \"\"")
	}

	if p.Distribution == "kind" {
		g = append(g, "kind cluster detected: set runtime_class: \"\" and imagePullPolicy: IfNotPresent")
	}

	if p.CNI.Name == "unknown" {
		g = append(g, "WARNING: CNI plugin could not be detected — NetworkPolicy support is unknown; verify manually before relying on egress controls")
	}

	if p.Extensions.Istio {
		g = append(g, "Istio detected: NetworkPolicy egress rules must include namespaceSelector for istio-system; mTLS available between pods")
	}

	if p.NetworkPolicy.Supported && !p.NetworkPolicy.InUse {
		g = append(g, "NetworkPolicy is supported but none exist yet — generated policies will be the first; test egress rules carefully")
	}

	hasRWX := false
	for _, sc := range p.StorageClasses {
		if sc.RWXCapable {
			hasRWX = true
			g = append(g, fmt.Sprintf("RWX storage (inferred) via StorageClass %q — verify actual RWX support with the CSI driver before use", sc.Name))
		}
	}
	if !hasRWX {
		g = append(g, "No RWX-capable StorageClass inferred — avoid shared volume mounts across replicas (verify with cluster admin)")
	}

	if p.Quota != nil {
		g = append(g, fmt.Sprintf("ResourceQuota active in namespace %q: CPU limit %s, memory limit %s",
			p.Namespace, p.Quota.CPULimit, p.Quota.MemoryLimit))
	}

	if p.PodSecurity == "restricted" {
		g = append(g, "Namespace enforces restricted PodSecurity — containers must run as non-root with no privilege escalation")
	}

	if p.Extensions.CertManager {
		g = append(g, "cert-manager available — TLS certificates can be provisioned automatically")
	}

	return g
}
