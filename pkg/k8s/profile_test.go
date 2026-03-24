package k8s

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// ---------- detectDistribution ----------

func TestDetectDistribution(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"eks", map[string]string{"eks.amazonaws.com/nodegroup": "ng-1"}, "eks"},
		{"gke", map[string]string{"cloud.google.com/gke-nodepool": "pool-1"}, "gke"},
		{"aks", map[string]string{"kubernetes.azure.com/agentpool": "pool1"}, "aks"},
		{"k0s", map[string]string{"node.k0sproject.io/role": "worker"}, "k0s"},
		{"k3s", map[string]string{"node.kubernetes.io/instance-type": "K3S-small"}, "k3s"},
		{"vanilla", map[string]string{"some-label": "value"}, "vanilla"},
		{"no labels", nil, "vanilla"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := []corev1.Node{{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: tt.labels},
			}}
			got := detectDistribution(nodes)
			if got != tt.want {
				t.Errorf("detectDistribution() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectDistribution_Empty(t *testing.T) {
	got := detectDistribution(nil)
	if got != "vanilla" {
		t.Errorf("detectDistribution(nil) = %q, want vanilla", got)
	}
}

// ---------- isRWXCapable ----------

func TestIsRWXCapable(t *testing.T) {
	tests := []struct {
		provisioner string
		want        bool
	}{
		{"efs.csi.aws.com", true},
		{"nfs-subdir-external-provisioner", true},
		{"file.csi.azure.com", false},
		{"disk.csi.azure.com", false},
		{"kubernetes.io/azure-file", true},
		{"rook-ceph.cephfs.csi.ceph.com", true},
		{"kubernetes.io/glusterfs", true},
		{"rbd.csi.ceph.com", true},
		{"ebs.csi.aws.com", false},
		{"rancher.io/local-path", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.provisioner, func(t *testing.T) {
			got := isRWXCapable(tt.provisioner)
			if got != tt.want {
				t.Errorf("isRWXCapable(%q) = %v, want %v", tt.provisioner, got, tt.want)
			}
		})
	}
}

// ---------- classifyExtensions ----------

func TestClassifyExtensions(t *testing.T) {
	items := []unstructured.Unstructured{
		{Object: map[string]any{"metadata": map[string]any{"name": "virtualservices.networking.istio.io"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "certificates.cert-manager.io"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "servicemonitors.monitoring.coreos.com"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "externalsecrets.external-secrets.io"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "applications.argoproj.io"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "gateways.gateway.networking.k8s.io"}}},
		{Object: map[string]any{"metadata": map[string]any{"name": "somecrd.custom.example.com"}}},
	}

	crdList := &unstructured.UnstructuredList{Items: items}
	ext := classifyExtensions(crdList)

	if !ext.Istio {
		t.Error("expected Istio=true")
	}
	if !ext.CertManager {
		t.Error("expected CertManager=true")
	}
	if !ext.PrometheusOp {
		t.Error("expected PrometheusOp=true")
	}
	if !ext.ExternalSecrets {
		t.Error("expected ExternalSecrets=true")
	}
	if !ext.ArgoCD {
		t.Error("expected ArgoCD=true")
	}
	if !ext.GatewayAPI {
		t.Error("expected GatewayAPI=true")
	}
	if len(ext.OtherCRDGroups) != 1 || ext.OtherCRDGroups[0] != "custom.example.com" {
		t.Errorf("OtherCRDGroups = %v, want [custom.example.com]", ext.OtherCRDGroups)
	}
}

func TestClassifyExtensions_Truncation(t *testing.T) {
	items := make([]unstructured.Unstructured, 0, 25)
	for i := range 25 {
		name := "crd" + string(rune('a'+i)) + ".group" + string(rune('a'+i)) + ".example.com"
		items = append(items, unstructured.Unstructured{
			Object: map[string]any{"metadata": map[string]any{"name": name}},
		})
	}

	crdList := &unstructured.UnstructuredList{Items: items}
	ext := classifyExtensions(crdList)

	// Should be truncated to 20 + 1 "... and N more" entry = 21
	if len(ext.OtherCRDGroups) != 21 {
		t.Errorf("expected 21 OtherCRDGroups (20 + truncation), got %d", len(ext.OtherCRDGroups))
	}
}

func TestClassifyExtensions_Empty(t *testing.T) {
	crdList := &unstructured.UnstructuredList{}
	ext := classifyExtensions(crdList)

	if ext.Istio || ext.CertManager || ext.PrometheusOp || ext.ExternalSecrets || ext.ArgoCD || ext.GatewayAPI {
		t.Error("expected all extensions false for empty list")
	}
}

func TestClassifyExtensions_IstioVariants(t *testing.T) {
	tests := []struct {
		name    string
		crdName string
	}{
		{"networking.istio.io", "virtualservices.networking.istio.io"},
		{"security.istio.io", "authorizationpolicies.security.istio.io"},
		{"suffix .istio.io", "telemetries.telemetry.istio.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := []unstructured.Unstructured{
				{Object: map[string]any{"metadata": map[string]any{"name": tt.crdName}}},
			}
			ext := classifyExtensions(&unstructured.UnstructuredList{Items: items})
			if !ext.Istio {
				t.Errorf("expected Istio=true for CRD %q", tt.crdName)
			}
		})
	}
}

// ---------- deriveGuidance ----------

func TestDeriveGuidance_GVisorAvailable(t *testing.T) {
	p := &ClusterProfile{GVisor: true, CNI: CNIInfo{Name: "cilium"}}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "Use runtime_class: gvisor for untrusted workflow steps" {
			found = true
		}
	}
	if !found {
		t.Error("expected gVisor guidance when GVisor=true")
	}
}

func TestDeriveGuidance_GVisorUnavailable(t *testing.T) {
	p := &ClusterProfile{GVisor: false, CNI: CNIInfo{Name: "cilium"}}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "gVisor not available — omit runtime_class or set it to \"\"" {
			found = true
		}
	}
	if !found {
		t.Error("expected gVisor unavailable guidance")
	}
}

func TestDeriveGuidance_UnknownCNI(t *testing.T) {
	p := &ClusterProfile{CNI: CNIInfo{Name: "unknown"}}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "WARNING: CNI plugin could not be detected — NetworkPolicy support is unknown; verify manually before relying on egress controls" {
			found = true
		}
	}
	if !found {
		t.Error("expected unknown CNI warning")
	}
}

func TestDeriveGuidance_Istio(t *testing.T) {
	p := &ClusterProfile{
		CNI:        CNIInfo{Name: "cilium"},
		Extensions: ExtensionSet{Istio: true},
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "Istio detected: NetworkPolicy egress rules must include namespaceSelector for istio-system; mTLS available between pods" {
			found = true
		}
	}
	if !found {
		t.Error("expected Istio guidance")
	}
}

func TestDeriveGuidance_NetworkPolicySupportedNotInUse(t *testing.T) {
	p := &ClusterProfile{
		CNI:           CNIInfo{Name: "cilium"},
		NetworkPolicy: NetPolInfo{Supported: true, InUse: false},
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "NetworkPolicy is supported but none exist yet — generated policies will be the first; test egress rules carefully" {
			found = true
		}
	}
	if !found {
		t.Error("expected NetworkPolicy guidance")
	}
}

func TestDeriveGuidance_RestrictedPodSecurity(t *testing.T) {
	p := &ClusterProfile{
		CNI:         CNIInfo{Name: "cilium"},
		PodSecurity: "restricted",
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "Namespace enforces restricted PodSecurity — containers must run as non-root with no privilege escalation" {
			found = true
		}
	}
	if !found {
		t.Error("expected restricted PodSecurity guidance")
	}
}

func TestDeriveGuidance_CertManager(t *testing.T) {
	p := &ClusterProfile{
		CNI:        CNIInfo{Name: "cilium"},
		Extensions: ExtensionSet{CertManager: true},
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "cert-manager available — TLS certificates can be provisioned automatically" {
			found = true
		}
	}
	if !found {
		t.Error("expected cert-manager guidance")
	}
}

func TestDeriveGuidance_RWXStorageClass(t *testing.T) {
	p := &ClusterProfile{
		CNI: CNIInfo{Name: "cilium"},
		StorageClasses: []StorageClassInfo{
			{Name: "efs-sc", Provisioner: "efs.csi.aws.com", RWXCapable: true},
		},
	}
	g := deriveGuidance(p)
	foundRWX := false
	for _, s := range g {
		if s == "RWX storage (inferred) via StorageClass \"efs-sc\" — verify actual RWX support with the CSI driver before use" {
			foundRWX = true
		}
	}
	if !foundRWX {
		t.Error("expected RWX StorageClass guidance")
	}
}

func TestDeriveGuidance_NoRWXStorageClass(t *testing.T) {
	p := &ClusterProfile{
		CNI: CNIInfo{Name: "cilium"},
		StorageClasses: []StorageClassInfo{
			{Name: "ebs-sc", Provisioner: "ebs.csi.aws.com", RWXCapable: false},
		},
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "No RWX-capable StorageClass inferred — avoid shared volume mounts across replicas (verify with cluster admin)" {
			found = true
		}
	}
	if !found {
		t.Error("expected no-RWX guidance")
	}
}

func TestDeriveGuidance_Quota(t *testing.T) {
	p := &ClusterProfile{
		CNI:       CNIInfo{Name: "cilium"},
		Namespace: "prod",
		Quota:     &QuotaSummary{CPULimit: "4", MemoryLimit: "8Gi"},
	}
	g := deriveGuidance(p)
	found := false
	for _, s := range g {
		if s == "ResourceQuota active in namespace \"prod\": CPU limit 4, memory limit 8Gi" {
			found = true
		}
	}
	if !found {
		t.Error("expected ResourceQuota guidance")
	}
}

// ---------- profileCNI ----------

func TestProfileCNI(t *testing.T) {
	tests := []struct {
		name    string
		podName string
		labels  map[string]string
		wantCNI string
	}{
		{"calico", "calico-node-abc", map[string]string{"k8s-app": "calico-node"}, "calico"},
		{"cilium", "cilium-xyz", map[string]string{"k8s-app": "cilium"}, "cilium"},
		{"kube-router", "kube-router-abc", map[string]string{"k8s-app": "kube-router"}, "kube-router"},
		{"weave", "weave-net-123", map[string]string{}, "weave"},
		{"flannel", "flannel-abc", map[string]string{}, "flannel"},
		{"kindnet", "kindnet-abc", map[string]string{}, "kindnet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.podName,
					Namespace: "kube-system",
					Labels:    tt.labels,
				},
			}
			cs := kubefake.NewClientset(pod)
			client := &Client{Clientset: cs, Config: &rest.Config{}}
			profile := &ClusterProfile{}
			err := profileCNI(context.Background(), client, profile)
			if err != nil {
				t.Fatalf("profileCNI: %v", err)
			}
			if profile.CNI.Name != tt.wantCNI {
				t.Errorf("CNI.Name = %q, want %q", profile.CNI.Name, tt.wantCNI)
			}
		})
	}
}

func TestProfileCNI_Unknown(t *testing.T) {
	cs := kubefake.NewClientset()
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{}
	err := profileCNI(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileCNI: %v", err)
	}
	if profile.CNI.Name != "unknown" {
		t.Errorf("CNI.Name = %q, want unknown", profile.CNI.Name)
	}
}

// ---------- profileRuntimeClasses ----------

func TestProfileRuntimeClasses(t *testing.T) {
	rcs := []nodev1.RuntimeClass{
		{ObjectMeta: metav1.ObjectMeta{Name: "gvisor"}, Handler: "runsc"},
		{ObjectMeta: metav1.ObjectMeta{Name: "kata"}, Handler: "kata-runtime"},
	}
	cs := kubefake.NewClientset(&nodev1.RuntimeClassList{Items: rcs})
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{RuntimeClasses: []RuntimeClassInfo{}}

	err := profileRuntimeClasses(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileRuntimeClasses: %v", err)
	}
	if len(profile.RuntimeClasses) != 2 {
		t.Fatalf("expected 2 runtime classes, got %d", len(profile.RuntimeClasses))
	}
	if !profile.GVisor {
		t.Error("expected GVisor=true with gvisor RuntimeClass")
	}
}

func TestProfileRuntimeClasses_NoGVisor(t *testing.T) {
	rcs := []nodev1.RuntimeClass{
		{ObjectMeta: metav1.ObjectMeta{Name: "kata"}, Handler: "kata-runtime"},
	}
	cs := kubefake.NewClientset(&nodev1.RuntimeClassList{Items: rcs})
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{RuntimeClasses: []RuntimeClassInfo{}}

	err := profileRuntimeClasses(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileRuntimeClasses: %v", err)
	}
	if profile.GVisor {
		t.Error("expected GVisor=false without gvisor RuntimeClass")
	}
}

// ---------- profileStorageClasses ----------

func TestProfileStorageClasses(t *testing.T) {
	retain := corev1.PersistentVolumeReclaimRetain
	expand := true
	scs := []storagev1.StorageClass{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "efs-sc",
				Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
			},
			Provisioner:          "efs.csi.aws.com",
			ReclaimPolicy:        &retain,
			AllowVolumeExpansion: &expand,
		},
		{
			ObjectMeta:  metav1.ObjectMeta{Name: "ebs-sc"},
			Provisioner: "ebs.csi.aws.com",
		},
	}
	cs := kubefake.NewClientset(&storagev1.StorageClassList{Items: scs})
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{StorageClasses: []StorageClassInfo{}}

	err := profileStorageClasses(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileStorageClasses: %v", err)
	}
	if len(profile.StorageClasses) != 2 {
		t.Fatalf("expected 2 storage classes, got %d", len(profile.StorageClasses))
	}

	// Find by name since fake client may not preserve order.
	scByName := map[string]StorageClassInfo{}
	for _, sc := range profile.StorageClasses {
		scByName[sc.Name] = sc
	}

	efs := scByName["efs-sc"]
	if !efs.IsDefault {
		t.Error("efs-sc should be default")
	}
	if efs.ReclaimPolicy != "Retain" {
		t.Errorf("efs-sc ReclaimPolicy = %q, want Retain", efs.ReclaimPolicy)
	}
	if !efs.AllowVolumeExpansion {
		t.Error("efs-sc should allow volume expansion")
	}
	if !efs.RWXCapable {
		t.Error("efs-sc should be RWX capable")
	}

	ebs := scByName["ebs-sc"]
	if ebs.IsDefault {
		t.Error("ebs-sc should not be default")
	}
	if ebs.ReclaimPolicy != "Delete" {
		t.Errorf("ebs-sc ReclaimPolicy = %q, want Delete", ebs.ReclaimPolicy)
	}
	if ebs.RWXCapable {
		t.Error("ebs-sc should not be RWX capable")
	}

	if profile.RWXNote == "" {
		t.Error("expected non-empty RWXNote when StorageClasses exist")
	}
}

// ---------- profileCSIDrivers ----------

func TestProfileCSIDrivers(t *testing.T) {
	drivers := []storagev1.CSIDriver{
		{ObjectMeta: metav1.ObjectMeta{Name: "ebs.csi.aws.com"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "efs.csi.aws.com"}},
	}
	cs := kubefake.NewClientset(&storagev1.CSIDriverList{Items: drivers})
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{CSIDrivers: []string{}}

	err := profileCSIDrivers(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileCSIDrivers: %v", err)
	}
	if len(profile.CSIDrivers) != 2 {
		t.Fatalf("expected 2 CSI drivers, got %d", len(profile.CSIDrivers))
	}
}

// ---------- profileNetworkPolicy ----------

func TestProfileNetworkPolicy_InUse(t *testing.T) {
	cs := kubefake.NewClientset()
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{CNI: CNIInfo{Name: "cilium", NetworkPolicySupported: true}}

	profileNetworkPolicy(context.Background(), client, profile)

	if !profile.NetworkPolicy.Supported {
		t.Error("expected NetworkPolicy.Supported=true")
	}
	if profile.NetworkPolicy.InUse {
		t.Error("expected NetworkPolicy.InUse=false with no policies")
	}
}

// ---------- profileIngress ----------

func TestProfileIngress(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ingress-nginx-controller-abc", Namespace: "ingress-nginx",
				Labels: map[string]string{"app.kubernetes.io/name": "ingress-nginx"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "traefik-abc", Namespace: "traefik",
				Labels: map[string]string{"app.kubernetes.io/name": "traefik"},
			},
		},
	}
	// Build fake clientset with pods.
	podList := &corev1.PodList{Items: pods}
	cs := kubefake.NewClientset(podList)
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{Ingress: []string{}}

	profileIngress(context.Background(), client, profile)

	if len(profile.Ingress) != 2 {
		t.Fatalf("expected 2 ingress controllers, got %v", profile.Ingress)
	}
}

// ---------- profileNamespaceDetails ----------

func TestProfileNamespaceDetails(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "prod",
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
		},
	}
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "default-quota", Namespace: "prod"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
				corev1.ResourceLimitsCPU:      resource.MustParse("4"),
				corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("8Gi"),
				corev1.ResourcePods:           resource.MustParse("20"),
			},
		},
	}
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "default-limits", Namespace: "prod"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		},
	}

	cs := kubefake.NewClientset(ns, quota, lr)
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{}

	err := profileNamespaceDetails(context.Background(), client, profile, "prod")
	if err != nil {
		t.Fatalf("profileNamespaceDetails: %v", err)
	}

	if profile.PodSecurity != "restricted" {
		t.Errorf("PodSecurity = %q, want restricted", profile.PodSecurity)
	}
	if profile.Quota == nil {
		t.Fatal("expected Quota to be set")
	}
	if profile.Quota.CPULimit != "4" {
		t.Errorf("Quota.CPULimit = %q, want 4", profile.Quota.CPULimit)
	}
	if profile.Quota.MemoryLimit != "8Gi" {
		t.Errorf("Quota.MemoryLimit = %q, want 8Gi", profile.Quota.MemoryLimit)
	}
	if profile.Quota.MaxPods != 20 {
		t.Errorf("Quota.MaxPods = %d, want 20", profile.Quota.MaxPods)
	}
	if profile.LimitRange == nil {
		t.Fatal("expected LimitRange to be set")
	}
	if profile.LimitRange.DefaultCPULimit != "500m" {
		t.Errorf("LimitRange.DefaultCPULimit = %q, want 500m", profile.LimitRange.DefaultCPULimit)
	}
	if profile.LimitRange.DefaultMemoryRequest != "128Mi" {
		t.Errorf("LimitRange.DefaultMemoryRequest = %q, want 128Mi", profile.LimitRange.DefaultMemoryRequest)
	}
}

func TestProfileNamespaceDetails_EmptyNamespace(t *testing.T) {
	cs := kubefake.NewClientset()
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{}

	err := profileNamespaceDetails(context.Background(), client, profile, "")
	if err != nil {
		t.Fatalf("expected nil error for empty namespace, got %v", err)
	}
	if profile.PodSecurity != "" {
		t.Errorf("expected empty PodSecurity, got %q", profile.PodSecurity)
	}
}

// ---------- profileNodes ----------

func TestProfileNodes(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "worker-1",
			Labels: map[string]string{"eks.amazonaws.com/nodegroup": "ng-1"},
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			NodeInfo: corev1.NodeSystemInfo{
				OperatingSystem:         "linux",
				Architecture:            "amd64",
				KubeletVersion:          "v1.29.0",
				KernelVersion:           "5.15.0",
				ContainerRuntimeVersion: "containerd://1.7.0",
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
		},
	}

	cs := kubefake.NewClientset(node)
	client := &Client{Clientset: cs, Config: &rest.Config{}}
	profile := &ClusterProfile{Nodes: []NodeInfo{}}

	err := profileNodes(context.Background(), client, profile)
	if err != nil {
		t.Fatalf("profileNodes: %v", err)
	}
	if len(profile.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(profile.Nodes))
	}

	n := profile.Nodes[0]
	if n.Name != "worker-1" {
		t.Errorf("Name = %q", n.Name)
	}
	if !n.Ready {
		t.Error("expected Ready=true")
	}
	if n.OS != "linux" {
		t.Errorf("OS = %q", n.OS)
	}
	if n.Arch != "amd64" {
		t.Errorf("Arch = %q", n.Arch)
	}
	if len(n.Taints) != 1 {
		t.Errorf("expected 1 taint, got %d", len(n.Taints))
	}
	if profile.Distribution != "eks" {
		t.Errorf("Distribution = %q, want eks", profile.Distribution)
	}
}

// ---------- ExoskeletonInfo JSON ----------

func TestExoskeletonInfo_OmittedWhenNil(t *testing.T) {
	profile := &ClusterProfile{
		K8sVersion: "v1.29.0",
		Namespace:  "default",
		CNI:        CNIInfo{Name: "cilium"},
	}

	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, ok := raw["exoskeleton"]; ok {
		t.Error("expected exoskeleton field to be omitted when nil")
	}
}

func TestExoskeletonInfo_PresentWhenSet(t *testing.T) {
	profile := &ClusterProfile{
		K8sVersion: "v1.29.0",
		Namespace:  "default",
		CNI:        CNIInfo{Name: "cilium"},
		Exoskeleton: &ExoskeletonInfo{
			Enabled: true,
			Services: []ExoskeletonServiceInfo{
				{
					Name:      "postgres",
					Host:      "pg.example.com",
					Port:      "5432",
					Protocol:  "postgresql",
					Available: true,
				},
				{
					Name:          "nats",
					Host:          "nats.example.com",
					Port:          "4222",
					Protocol:      "nats",
					Available:     true,
					SPIFFEEnabled: true,
				},
			},
			Auth: ExoskeletonAuthInfo{
				Enabled: true,
				Issuer:  "https://keycloak.example.com/realms/tentacular",
			},
		},
	}

	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	exo, ok := raw["exoskeleton"]
	if !ok {
		t.Fatal("expected exoskeleton field to be present")
	}

	exoMap, ok := exo.(map[string]any)
	if !ok {
		t.Fatal("expected exoskeleton to be an object")
	}

	if exoMap["enabled"] != true {
		t.Error("expected exoskeleton.enabled=true")
	}

	services, ok := exoMap["services"].([]any)
	if !ok || len(services) != 2 {
		t.Fatalf("expected 2 services, got %v", exoMap["services"])
	}

	// Verify first service
	svc0 := services[0].(map[string]any)
	if svc0["name"] != "postgres" {
		t.Errorf("services[0].name = %v, want postgres", svc0["name"])
	}
	if svc0["available"] != true {
		t.Errorf("services[0].available = %v, want true", svc0["available"])
	}

	// Verify NATS service has spiffeEnabled
	svc1 := services[1].(map[string]any)
	if svc1["spiffeEnabled"] != true {
		t.Errorf("services[1].spiffeEnabled = %v, want true", svc1["spiffeEnabled"])
	}

	// Verify auth
	auth, ok := exoMap["auth"].(map[string]any)
	if !ok {
		t.Fatal("expected auth to be an object")
	}
	if auth["enabled"] != true {
		t.Error("expected auth.enabled=true")
	}
	if auth["issuer"] != "https://keycloak.example.com/realms/tentacular" {
		t.Errorf("auth.issuer = %v", auth["issuer"])
	}
}

func TestExoskeletonInfo_SPIFFEEnabledOmittedWhenFalse(t *testing.T) {
	svc := ExoskeletonServiceInfo{
		Name:      "postgres",
		Host:      "pg.example.com",
		Port:      "5432",
		Protocol:  "postgresql",
		Available: true,
	}

	data, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, ok := raw["spiffeEnabled"]; ok {
		t.Error("expected spiffeEnabled to be omitted when false")
	}
}
