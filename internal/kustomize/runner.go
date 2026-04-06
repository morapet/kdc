package kustomize

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Build runs "kustomize build <path>" and returns the combined YAML output.
// If the kustomize binary is not found or exits non-zero, an error is returned.
func Build(path string) ([]byte, error) {
	cmd := exec.Command("kustomize", "build", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("kustomize build failed: %s", stderr.String())
		}
		// Binary not found or similar
		return nil, fmt.Errorf("kustomize not found in PATH; install from https://kubectl.docs.kubernetes.io/installation/kustomize/: %w", err)
	}
	return stdout.Bytes(), nil
}
