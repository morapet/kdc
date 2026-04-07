// Package filter implements a declarative rules engine for kdc.
// Rules are loaded from a kdc-filters.yaml file and applied during translation
// to skip or replace Kubernetes resources and containers.
//
// Supported filter types:
//   - containers.skip  – drop a container from every pod that contains it
//   - containers.replace – swap a container for a local compose service
//   - initContainers.skip – drop init containers (matched by name/image)
//   - resources.skip  – skip entire K8s resource kinds or named resources
package filter

import (
	"fmt"
	"os"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"
)

// Config is the root structure of a kdc-filters.yaml file.
type Config struct {
	Containers     ContainerFilters     `yaml:"containers"`
	InitContainers InitContainerFilters `yaml:"initContainers"`
	Resources      ResourceFilters      `yaml:"resources"`
}

// ContainerFilters holds rules that operate on regular (non-init) containers.
type ContainerFilters struct {
	// Skip lists matchers for containers to remove entirely.
	Skip []ContainerMatcher `yaml:"skip"`
	// Replace lists rules that swap a matched container for a local compose service.
	Replace []ContainerReplacement `yaml:"replace"`
}

// InitContainerFilters holds rules for init containers.
// (Init containers are not translated to compose services by default; this
// section lets you also explicitly document which ones are intentionally dropped.)
type InitContainerFilters struct {
	Skip []ContainerMatcher `yaml:"skip"`
}

// ContainerMatcher matches a container by name and/or image glob.
// If both Name and Image are set, BOTH must match.
// If only one is set, that one must match.
// Supports * (matches any chars incl. /) and ? (matches one char).
type ContainerMatcher struct {
	// Name is a glob pattern matched against the container name.
	Name string `yaml:"name"`
	// Image is a glob pattern matched against the container image (including tag).
	Image string `yaml:"image"`
}

// ContainerReplacement replaces a matched container with a locally-runnable
// compose service. The replacement service is injected once into the project
// (de-duplicated by service name across all deployments).
type ContainerReplacement struct {
	Match ContainerMatcher `yaml:"match"`
	// With is the compose service definition that replaces the matched container.
	With ReplaceService `yaml:"with"`
}

// ReplaceService is a subset of compose ServiceConfig used for replacements.
// Keeping it narrow avoids YAML unmarshalling complexity of the full type.
type ReplaceService struct {
	// Name is the compose service name for the injected service (required).
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
	// Environment is a plain map (not compose MappingWithEquals) for user convenience.
	Environment map[string]string               `yaml:"environment"`
	Ports       []comptypes.ServicePortConfig   `yaml:"ports"`
	Volumes     []comptypes.ServiceVolumeConfig `yaml:"volumes"`
}

// ToServiceConfig converts the ReplaceService into a compose-go ServiceConfig.
func (r *ReplaceService) ToServiceConfig() comptypes.ServiceConfig {
	env := make(comptypes.MappingWithEquals, len(r.Environment))
	for k, v := range r.Environment {
		val := v
		env[k] = &val
	}
	return comptypes.ServiceConfig{
		Name:        r.Name,
		Image:       r.Image,
		Environment: env,
		Ports:       r.Ports,
		Volumes:     r.Volumes,
	}
}

// ResourceFilters holds rules that operate on whole Kubernetes resources.
type ResourceFilters struct {
	// Skip lists resources to exclude from translation entirely.
	Skip []ResourceMatcher `yaml:"skip"`
}

// ResourceMatcher matches a K8s resource by kind and/or name glob.
// If both Kind and Name are set, BOTH must match.
// Leave a field empty to match any value for that field.
type ResourceMatcher struct {
	// Kind is the Kubernetes resource kind (e.g. HorizontalPodAutoscaler).
	Kind string `yaml:"kind"`
	// Name is a glob pattern matched against the resource metadata.name.
	Name string `yaml:"name"`
}

// Load reads and parses a filter config file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read filter config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse filter config %q: %w", path, err)
	}
	return &cfg, nil
}
