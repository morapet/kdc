package override

import (
	"fmt"
	"os"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"
)

// Apply loads the YAML at overridePath, parses it as a partial compose Project,
// and merges it onto base. User-supplied values win on conflicts.
// Ports and volume slices on services are APPENDED (not replaced) so generated
// entries are preserved.
// If overridePath is empty, base is returned unchanged.
func Apply(base *comptypes.Project, overridePath string) (*comptypes.Project, error) {
	if overridePath == "" {
		return base, nil
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		return nil, fmt.Errorf("read overrides file %q: %w", overridePath, err)
	}

	var override comptypes.Project
	if err := yaml.Unmarshal(data, &override); err != nil {
		return nil, fmt.Errorf("parse overrides file %q: %w", overridePath, err)
	}

	mergeProjects(base, &override)
	return base, nil
}

// mergeProjects merges override fields onto base in-place.
// - Services: merged per-service (ports/volumes appended, scalars override-wins).
// - Volumes/Secrets/Configs: override entries are added/replaced.
// - Project name: override wins if non-empty.
func mergeProjects(base, override *comptypes.Project) {
	if override.Name != "" {
		base.Name = override.Name
	}

	// Merge services.
	if base.Services == nil {
		base.Services = comptypes.Services{}
	}
	for name, overrideSvc := range override.Services {
		if baseSvc, exists := base.Services[name]; exists {
			base.Services[name] = mergeService(baseSvc, overrideSvc)
		} else {
			base.Services[name] = overrideSvc
		}
	}

	// Merge top-level volumes.
	if base.Volumes == nil {
		base.Volumes = comptypes.Volumes{}
	}
	for k, v := range override.Volumes {
		base.Volumes[k] = v
	}

	// Merge top-level secrets.
	if base.Secrets == nil {
		base.Secrets = comptypes.Secrets{}
	}
	for k, v := range override.Secrets {
		base.Secrets[k] = v
	}

	// Merge top-level configs.
	if base.Configs == nil {
		base.Configs = map[string]comptypes.ConfigObjConfig{}
	}
	for k, v := range override.Configs {
		base.Configs[k] = v
	}
}

// mergeService merges override service fields onto base service.
// Ports and Volumes are appended; all other non-zero override fields win.
func mergeService(base, override comptypes.ServiceConfig) comptypes.ServiceConfig {
	// Scalar overrides.
	if override.Image != "" {
		base.Image = override.Image
	}
	if override.Build != nil {
		base.Build = override.Build
	}
	if len(override.Entrypoint) > 0 {
		base.Entrypoint = override.Entrypoint
	}
	if len(override.Command) > 0 {
		base.Command = override.Command
	}
	if override.WorkingDir != "" {
		base.WorkingDir = override.WorkingDir
	}
	if override.Restart != "" {
		base.Restart = override.Restart
	}
	if override.User != "" {
		base.User = override.User
	}
	if override.HealthCheck != nil {
		base.HealthCheck = override.HealthCheck
	}
	if override.Deploy != nil {
		base.Deploy = override.Deploy
	}

	// Environment: override wins per key.
	if base.Environment == nil {
		base.Environment = comptypes.MappingWithEquals{}
	}
	for k, v := range override.Environment {
		base.Environment[k] = v
	}

	// Labels: override wins per key.
	if base.Labels == nil {
		base.Labels = comptypes.Labels{}
	}
	for k, v := range override.Labels {
		base.Labels[k] = v
	}

	// Ports: append, deduplicate by target port (override wins on collision).
	base.Ports = appendPortConfigs(base.Ports, override.Ports)

	// Volumes: append, deduplicate by target path (override wins on collision).
	base.Volumes = appendVolumeConfigs(base.Volumes, override.Volumes)

	// Networks: add override networks.
	if base.Networks == nil {
		base.Networks = map[string]*comptypes.ServiceNetworkConfig{}
	}
	for k, v := range override.Networks {
		base.Networks[k] = v
	}

	return base
}

// appendPortConfigs merges src into dst, deduplicating by target port number.
// src entries win if there is a collision.
func appendPortConfigs(dst, src []comptypes.ServicePortConfig) []comptypes.ServicePortConfig {
	byTarget := make(map[uint32]int, len(dst))
	result := make([]comptypes.ServicePortConfig, len(dst))
	copy(result, dst)
	for i, p := range result {
		byTarget[p.Target] = i
	}
	for _, p := range src {
		if idx, exists := byTarget[p.Target]; exists {
			result[idx] = p // override wins
		} else {
			byTarget[p.Target] = len(result)
			result = append(result, p)
		}
	}
	return result
}

// appendVolumeConfigs merges src into dst, deduplicating by target path.
// src entries win if there is a collision.
func appendVolumeConfigs(dst, src []comptypes.ServiceVolumeConfig) []comptypes.ServiceVolumeConfig {
	byTarget := make(map[string]int, len(dst))
	result := make([]comptypes.ServiceVolumeConfig, len(dst))
	copy(result, dst)
	for i, v := range result {
		byTarget[v.Target] = i
	}
	for _, v := range src {
		if idx, exists := byTarget[v.Target]; exists {
			result[idx] = v
		} else {
			byTarget[v.Target] = len(result)
			result = append(result, v)
		}
	}
	return result
}
