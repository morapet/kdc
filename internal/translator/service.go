package translator

import (
	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
)

// applyServiceAliases iterates over K8s Services in the registry and adds network
// aliases to matching compose services. A K8s Service's spec.selector is matched
// against the pod template labels of each workload (Deployment, StatefulSet, Pod).
// For each matching compose service, the K8s Service name is added as a network
// alias on the "default" network.
func applyServiceAliases(services comptypes.Services, reg *registry.ResourceRegistry) {
	for _, k8sSvc := range reg.Services {
		if len(k8sSvc.Spec.Selector) == 0 {
			continue
		}

		// Build the set of workload kind/name pairs matched by this selector.
		matched := matchServiceToWorkloads(k8sSvc.Spec.Selector, reg)
		if len(matched) == 0 {
			continue
		}

		// Apply alias to every compose service originating from a matched workload.
		for composeName, composeSvc := range services {
			sourceKind := composeSvc.Labels[kdctypes.AnnotationSourceKind]
			sourceName := composeSvc.Labels[kdctypes.AnnotationSourceName]
			key := sourceKind + "/" + sourceName

			if !matched[key] {
				continue
			}

			if composeSvc.Networks == nil {
				composeSvc.Networks = map[string]*comptypes.ServiceNetworkConfig{}
			}
			netCfg := composeSvc.Networks["default"]
			if netCfg == nil {
				netCfg = &comptypes.ServiceNetworkConfig{}
			}
			netCfg.Aliases = appendUnique(netCfg.Aliases, k8sSvc.Name)
			composeSvc.Networks["default"] = netCfg
			services[composeName] = composeSvc
		}
	}
}

// matchServiceToWorkloads returns a set of "kind/name" keys for workloads whose
// pod template labels satisfy all selector requirements. It checks Deployments,
// StatefulSets, and Pods in the registry.
func matchServiceToWorkloads(selector map[string]string, reg *registry.ResourceRegistry) map[string]bool {
	matched := map[string]bool{}

	for _, d := range reg.Deployments {
		if labelsMatchSelector(d.Spec.Template.Labels, selector) {
			matched["Deployment/"+d.Name] = true
		}
	}
	for _, s := range reg.StatefulSets {
		if labelsMatchSelector(s.Spec.Template.Labels, selector) {
			matched["StatefulSet/"+s.Name] = true
		}
	}
	for _, p := range reg.Pods {
		if labelsMatchSelector(p.Labels, selector) {
			matched["Pod/"+p.Name] = true
		}
	}

	return matched
}

// labelsMatchSelector returns true if all selector key/value pairs are present
// in labels with equal values.
func labelsMatchSelector(labels map[string]string, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// appendUnique appends s to slice only if it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
