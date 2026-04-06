package types

import "fmt"

// TranslationContext carries data needed across the translation pipeline.
type TranslationContext struct {
	// Namespace used when looking up ConfigMap/Secret references.
	// Defaults to "default" if absent from manifests.
	Namespace string

	// ProjectName is the compose project name.
	// Derived from the kustomize overlay directory basename if not set explicitly.
	ProjectName string
}

// Annotation keys written onto generated services so users can trace origins.
const (
	AnnotationSourceKind      = "kdc.io/source-kind"
	AnnotationSourceName      = "kdc.io/source-name"
	AnnotationSourceNamespace = "kdc.io/source-namespace"
)

// UnsupportedResourceWarning is emitted for K8s resource kinds that kdc does not translate.
type UnsupportedResourceWarning struct {
	APIVersion string
	Kind       string
	Name       string
}

func (w UnsupportedResourceWarning) Error() string {
	return fmt.Sprintf("unsupported resource: %s/%s name=%q (skipped)", w.APIVersion, w.Kind, w.Name)
}

// ResourceKey returns a canonical "namespace/name" key used for registry lookups.
func ResourceKey(namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return namespace + "/" + name
}
