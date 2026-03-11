package proxy

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// DefaultNamespace is the default namespace for the esm.sh module proxy.
	DefaultNamespace = "tentacular-support"

	// DefaultImage is the default esm.sh module proxy image.
	// Pinned to match the CLI's tntc cluster install default (GenerateModuleProxyManifests).
	DefaultImage = "ghcr.io/esm-dev/esm.sh:v136"

	// DeploymentName is the K8s Deployment name for the esm.sh proxy.
	DeploymentName = "esm-sh"

	// ServiceName is the K8s Service name for the esm.sh proxy.
	ServiceName = "esm-sh"

	// ContainerPort is the port the esm.sh proxy listens on.
	ContainerPort = int32(8080)
)

// Options holds configuration for the module proxy.
type Options struct {
	// Namespace is the K8s namespace to deploy into (typically tentacular-support).
	Namespace string

	// Image is the container image to use. Defaults to DefaultImage if empty.
	Image string

	// StorageSize is the size of the PVC for the cache. Empty means emptyDir.
	StorageSize string

	// StatusDeploymentName overrides the deployment name used by GetStatus.
	// When set (e.g., by the umbrella chart), GetStatus looks up this name
	// instead of the default "esm-sh".
	StatusDeploymentName string

	// StatusNamespace overrides the namespace used by GetStatus.
	// When set, GetStatus and Namespace() use this instead of Namespace.
	StatusNamespace string
}

func (o Options) image() string {
	if o.Image != "" {
		return o.Image
	}
	return DefaultImage
}

func (o Options) statusDeploymentName() string {
	if o.StatusDeploymentName != "" {
		return o.StatusDeploymentName
	}
	return DeploymentName
}

func (o Options) statusNamespace() string {
	if o.StatusNamespace != "" {
		return o.StatusNamespace
	}
	return o.Namespace
}

func (o Options) storageType() string {
	if o.StorageSize != "" {
		return "pvc"
	}
	return "emptydir"
}

// BuildDeployment returns the Deployment manifest for the esm.sh proxy.
func BuildDeployment(opts Options) *appsv1.Deployment {
	replicas := int32(1)
	volumes, volumeMounts := buildStorage(opts)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName,
			Namespace: opts.Namespace,
			Labels:    proxyLabels(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: proxyLabels(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: proxyLabels(),
				},
				Spec: corev1.PodSpec{
					Volumes: volumes,
					Containers: []corev1.Container{
						{
							Name:         "esm-sh",
							Image:        opts.image(),
							VolumeMounts: volumeMounts,
							Ports: []corev1.ContainerPort{
								{ContainerPort: ContainerPort, Protocol: corev1.ProtocolTCP},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("100m"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("512Mi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/status",
										Port: intstr.FromInt32(ContainerPort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		},
	}
}

// BuildService returns the Service manifest for the esm.sh proxy.
func BuildService(opts Options) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: opts.Namespace,
			Labels:    proxyLabels(),
		},
		Spec: corev1.ServiceSpec{
			Selector: proxyLabels(),
			Ports: []corev1.ServicePort{
				{
					Port:     ContainerPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

func proxyLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "tentacular",
		"app.kubernetes.io/name":       "esm-sh",
		"app.kubernetes.io/component":  "module-proxy",
	}
}

func buildStorage(opts Options) ([]corev1.Volume, []corev1.VolumeMount) {
	mountPath := "/app/cache"
	if opts.StorageSize == "" {
		// emptyDir -- ephemeral cache, lost on pod restart
		return []corev1.Volume{
				{
					Name:         "cache",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			}, []corev1.VolumeMount{
				{Name: "cache", MountPath: mountPath},
			}
	}

	// PVC -- persistent cache survives pod restarts
	return []corev1.Volume{
			{
				Name: "cache",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: DeploymentName + "-cache",
					},
				},
			},
		}, []corev1.VolumeMount{
			{Name: "cache", MountPath: mountPath},
		}
}
