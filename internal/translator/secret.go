package translator

import (
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
)

// buildSecretEnvIndex returns a map of "ns/name" -> { key: value } for all
// Secrets in the registry. The K8s JSON unmarshaller already base64-decodes
// []byte Data fields, so values are usable as plain strings directly.
func buildSecretEnvIndex(reg *registry.ResourceRegistry) map[string]map[string]string {
	index := make(map[string]map[string]string)
	for key, sec := range reg.Secrets {
		env := make(map[string]string, len(sec.Data))
		for k, v := range sec.Data {
			env[k] = string(v) // v is already decoded bytes
		}
		// Also include StringData (un-encoded), which takes precedence.
		for k, v := range sec.StringData {
			env[k] = v
		}
		index[key] = env
	}
	return index
}

// secretEnvKey returns the registry lookup key for a Secret ref.
func secretEnvKey(refName, defaultNamespace string) string {
	return kdctypes.ResourceKey(defaultNamespace, refName)
}
