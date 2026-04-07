package registry

import (
	"github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ResourceRegistry holds all parsed Kubernetes resources from a kustomize build, indexed
// for efficient lookup by downstream translators.
type ResourceRegistry struct {
	Deployments  []*appsv1.Deployment
	StatefulSets []*appsv1.StatefulSet
	Pods         []*corev1.Pod
	// ConfigMaps keyed by "namespace/name"
	ConfigMaps map[string]*corev1.ConfigMap
	// Secrets keyed by "namespace/name"
	Secrets map[string]*corev1.Secret
	// Services keyed by "namespace/name"
	Services map[string]*corev1.Service
	PVCs     []*corev1.PersistentVolumeClaim
}

// New returns an empty ResourceRegistry.
func New() *ResourceRegistry {
	return &ResourceRegistry{
		ConfigMaps: make(map[string]*corev1.ConfigMap),
		Secrets:    make(map[string]*corev1.Secret),
		Services:   make(map[string]*corev1.Service),
	}
}

// ConfigMap returns the ConfigMap for the given namespace and name, or nil if not found.
func (r *ResourceRegistry) ConfigMap(namespace, name string) *corev1.ConfigMap {
	return r.ConfigMaps[types.ResourceKey(namespace, name)]
}

// Secret returns the Secret for the given namespace and name, or nil if not found.
func (r *ResourceRegistry) Secret(namespace, name string) *corev1.Secret {
	return r.Secrets[types.ResourceKey(namespace, name)]
}

// AddConfigMap registers a ConfigMap using its namespace/name as key.
func (r *ResourceRegistry) AddConfigMap(cm *corev1.ConfigMap) {
	r.ConfigMaps[types.ResourceKey(cm.Namespace, cm.Name)] = cm
}

// AddSecret registers a Secret using its namespace/name as key.
func (r *ResourceRegistry) AddSecret(s *corev1.Secret) {
	r.Secrets[types.ResourceKey(s.Namespace, s.Name)] = s
}

// AddService registers a Service using its namespace/name as key.
func (r *ResourceRegistry) AddService(svc *corev1.Service) {
	r.Services[types.ResourceKey(svc.Namespace, svc.Name)] = svc
}

// Service returns the Service for the given namespace and name, or nil if not found.
func (r *ResourceRegistry) Service(namespace, name string) *corev1.Service {
	return r.Services[types.ResourceKey(namespace, name)]
}
