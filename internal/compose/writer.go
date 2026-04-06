package compose

import (
	"fmt"
	"os"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"
)

// Write serialises the compose Project to YAML and writes it to path.
// If path is "-", the output is written to stdout.
func Write(p *comptypes.Project, path string) error {
	data, err := marshal(p)
	if err != nil {
		return fmt.Errorf("marshal compose project: %w", err)
	}

	if path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// marshal encodes a compose Project to YAML bytes.
// We use sigs.k8s.io/yaml (which goes via JSON) because compose-go's types
// use json struct tags, not yaml tags.
func marshal(p *comptypes.Project) ([]byte, error) {
	// compose-go types use json tags, so marshal via JSON → YAML.
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	return data, nil
}
