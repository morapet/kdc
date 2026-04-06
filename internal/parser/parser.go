package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
	goyaml "gopkg.in/yaml.v3"
)

// typeMeta is used to peek at the apiVersion/kind/metadata of a raw document
// before full unmarshalling.
type typeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

// Parse splits rawYAML (multi-document) into individual Kubernetes resources,
// dispatches each to the appropriate registry bucket, and collects warnings for
// unsupported kinds. Unknown kinds are skipped without returning an error.
func Parse(raw []byte) (*registry.ResourceRegistry, []kdctypes.UnsupportedResourceWarning, error) {
	reg := registry.New()
	var warnings []kdctypes.UnsupportedResourceWarning

	decoder := goyaml.NewDecoder(bytes.NewReader(raw))
	for {
		// Decode next YAML document into a generic node.
		var node goyaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, fmt.Errorf("yaml decode error: %w", err)
		}

		// Re-encode this single document to YAML bytes, then convert to JSON
		// so we can use sigs.k8s.io/yaml's K8s-aware unmarshalling.
		docBytes, err := goyaml.Marshal(node.Content[0])
		if err != nil {
			return nil, nil, fmt.Errorf("yaml re-encode error: %w", err)
		}

		// Skip empty documents.
		if len(bytes.TrimSpace(docBytes)) == 0 || string(bytes.TrimSpace(docBytes)) == "null" {
			continue
		}

		jsonBytes, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("yaml-to-json error: %w", err)
		}

		// Peek at kind.
		var meta typeMeta
		if err := json.Unmarshal(jsonBytes, &meta); err != nil {
			return nil, nil, fmt.Errorf("typemeta unmarshal error: %w", err)
		}
		if meta.Kind == "" {
			continue // not a K8s resource
		}

		switch meta.Kind {
		case "Deployment":
			obj := &appsv1.Deployment{}
			if err := unmarshalK8s(jsonBytes, obj); err != nil {
				return nil, nil, fmt.Errorf("unmarshal Deployment %q: %w", meta.Metadata.Name, err)
			}
			reg.Deployments = append(reg.Deployments, obj)

		case "Pod":
			obj := &corev1.Pod{}
			if err := unmarshalK8s(jsonBytes, obj); err != nil {
				return nil, nil, fmt.Errorf("unmarshal Pod %q: %w", meta.Metadata.Name, err)
			}
			reg.Pods = append(reg.Pods, obj)

		case "ConfigMap":
			obj := &corev1.ConfigMap{}
			if err := unmarshalK8s(jsonBytes, obj); err != nil {
				return nil, nil, fmt.Errorf("unmarshal ConfigMap %q: %w", meta.Metadata.Name, err)
			}
			reg.AddConfigMap(obj)

		case "Secret":
			obj := &corev1.Secret{}
			if err := unmarshalK8s(jsonBytes, obj); err != nil {
				return nil, nil, fmt.Errorf("unmarshal Secret %q: %w", meta.Metadata.Name, err)
			}
			reg.AddSecret(obj)

		case "PersistentVolumeClaim":
			obj := &corev1.PersistentVolumeClaim{}
			if err := unmarshalK8s(jsonBytes, obj); err != nil {
				return nil, nil, fmt.Errorf("unmarshal PVC %q: %w", meta.Metadata.Name, err)
			}
			reg.PVCs = append(reg.PVCs, obj)

		default:
			warnings = append(warnings, kdctypes.UnsupportedResourceWarning{
				APIVersion: meta.APIVersion,
				Kind:       meta.Kind,
				Name:       meta.Metadata.Name,
			})
		}
	}

	return reg, warnings, nil
}

// unmarshalK8s unmarshals JSON bytes into a K8s runtime.Object via JSON codec.
func unmarshalK8s(jsonBytes []byte, obj runtime.Object) error {
	return json.Unmarshal(jsonBytes, obj)
}
