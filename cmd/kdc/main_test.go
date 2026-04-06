package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGenerate_GoldenFile runs the full pipeline on the testdata fixtures and
// compares the output against the committed golden file.
// Re-generate the golden file by setting the UPDATE_GOLDEN env var:
//
//	UPDATE_GOLDEN=1 go test ./cmd/kdc/
func TestGenerate_GoldenFile(t *testing.T) {
	// Locate kustomize binary.
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize not in PATH; skipping integration test")
	}

	repoRoot := filepath.Join("..", "..")
	goldenPath := filepath.Join(repoRoot, "testdata", "golden", "docker-compose.yaml")
	kustomizePath := filepath.Join(repoRoot, "testdata", "kustomize", "overlays", "dev")
	overridesPath := filepath.Join(repoRoot, "testdata", "kdc-overrides.yaml")

	outFile := filepath.Join(t.TempDir(), "docker-compose.yaml")

	err := runGenerate(kustomizePath, outFile, overridesPath, "dev", "default", false, false)
	if err != nil {
		t.Fatalf("runGenerate error: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		t.Logf("updated golden file %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("output does not match golden file.\nGot:\n%s\nWant:\n%s", got, want)
	}
}
