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
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize not in PATH; skipping integration test")
	}

	repoRoot := filepath.Join("..", "..")
	goldenPath := filepath.Join(repoRoot, "testdata", "golden", "docker-compose.yaml")
	kustomizePath := filepath.Join(repoRoot, "testdata", "kustomize", "overlays", "dev")
	overridesPath := filepath.Join(repoRoot, "testdata", "kdc-overrides.yaml")
	filtersPath := filepath.Join(repoRoot, "testdata", "kdc-filters.yaml")

	outFile := filepath.Join(t.TempDir(), "docker-compose.yaml")

	err := runGenerate(generateOpts{
		kustomizePath: kustomizePath,
		outputPath:    outFile,
		overridePath:  overridesPath,
		filtersPath:   filtersPath,
		projectName:   "dev",
		namespace:     "default",
	})
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

func TestGenerate_DryRun_NoConfigSecretFiles(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize not in PATH; skipping integration test")
	}

	repoRoot := filepath.Join("..", "..")
	kustomizePath := filepath.Join(repoRoot, "testdata", "kustomize", "overlays", "dev")
	overridesPath := filepath.Join(repoRoot, "testdata", "kdc-overrides.yaml")
	filtersPath := filepath.Join(repoRoot, "testdata", "kdc-filters.yaml")

	outDir := t.TempDir()
	outFile := filepath.Join(outDir, "docker-compose.yaml")

	err := runGenerate(generateOpts{
		kustomizePath: kustomizePath,
		outputPath:    outFile,
		overridePath:  overridesPath,
		filtersPath:   filtersPath,
		projectName:   "dev",
		namespace:     "default",
		dryRun:        true,
	})
	if err != nil {
		t.Fatalf("runGenerate dry-run error: %v", err)
	}

	// In dry-run mode, no .kdc directory should be written.
	kdcDir := filepath.Join(outDir, ".kdc")
	if _, err := os.Stat(kdcDir); err == nil {
		t.Errorf("dry-run should not write .kdc directory, but %s exists", kdcDir)
	}
}
