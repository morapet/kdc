package translator

import (
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
)

// buildConfigMapEnvIndex returns a map of "ns/name" -> { key: value } for all
// ConfigMaps in the registry, to be used when resolving envFrom/env.valueFrom refs.
func buildConfigMapEnvIndex(reg *registry.ResourceRegistry) map[string]map[string]string {
	index := make(map[string]map[string]string)
	for key, cm := range reg.ConfigMaps {
		env := make(map[string]string, len(cm.Data))
		for k, v := range cm.Data {
			env[k] = v
		}
		index[key] = env
	}
	return index
}

// configMapEnvKey returns the registry lookup key for a ConfigMap ref, using the
// given default namespace when the ref does not specify one.
func configMapEnvKey(refName, defaultNamespace string) string {
	return kdctypes.ResourceKey(defaultNamespace, refName)
}
