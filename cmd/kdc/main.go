package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/morapet/kdc/internal/compose"
	"github.com/morapet/kdc/internal/envfiles"
	"github.com/morapet/kdc/internal/kustomize"
	"github.com/morapet/kdc/internal/override"
	"github.com/morapet/kdc/internal/parser"
	"github.com/morapet/kdc/internal/translator"
	kdctypes "github.com/morapet/kdc/pkg/types"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "kdc",
		Short: "Generate a Docker Compose project from Kustomize manifests",
	}
	root.AddCommand(generateCmd())
	return root
}

func generateCmd() *cobra.Command {
	var (
		kustomizePath string
		outputPath    string
		overridePath  string
		projectName   string
		namespace     string
		verbose       bool
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate docker-compose.yaml from a Kustomize overlay",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(kustomizePath, outputPath, overridePath, projectName, namespace, verbose, dryRun)
		},
	}

	cmd.Flags().StringVarP(&kustomizePath, "kustomize", "k", "", "Path passed to `kustomize build` (required)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "docker-compose.yaml", "Output path (use \"-\" for stdout)")
	cmd.Flags().StringVar(&overridePath, "overrides", "", "Optional kdc-overrides.yaml path")
	cmd.Flags().StringVar(&projectName, "project", "", "Compose project name (default: overlay dir basename)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Kubernetes namespace for resource lookups")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print per-resource warnings to stderr")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print YAML to stdout, do not write file")

	_ = cmd.MarkFlagRequired("kustomize")
	return cmd
}

func runGenerate(kustomizePath, outputPath, overridePath, projectName, namespace string, verbose, dryRun bool) error {
	// Resolve project name.
	if projectName == "" {
		abs, err := filepath.Abs(kustomizePath)
		if err == nil {
			projectName = filepath.Base(abs)
		} else {
			projectName = filepath.Base(kustomizePath)
		}
	}

	// 1. Run kustomize build.
	rawYAML, err := kustomize.Build(kustomizePath)
	if err != nil {
		return err
	}

	// 2. Parse multi-document YAML into registry.
	reg, warnings, err := parser.Parse(rawYAML)
	if err != nil {
		return fmt.Errorf("parse kustomize output: %w", err)
	}

	// Report warnings.
	if len(warnings) > 0 {
		if verbose {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", w)
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: skipped %d unsupported resource type(s). Use --verbose for details.\n", len(warnings))
		}
	}

	// Require at least one workload.
	if len(reg.Deployments) == 0 && len(reg.Pods) == 0 {
		return fmt.Errorf("no translatable workload resources (Deployment or Pod) found in kustomize output")
	}

	// 3. Translate to compose Project.
	ctx := kdctypes.TranslationContext{
		Namespace:   namespace,
		ProjectName: projectName,
	}
	t := translator.New(reg, ctx)
	project, err := t.Translate()
	if err != nil {
		return fmt.Errorf("translate: %w", err)
	}

	// 4. Apply overrides.
	project, err = override.Apply(project, overridePath)
	if err != nil {
		return err
	}

	// 5. Write .env files for ConfigMaps and Secrets referenced via envFrom.
	// These go into .kdc/envs/ next to the output file (or cwd for stdout/dry-run).
	envDir := filepath.Join(filepath.Dir(outputPath), ".kdc", "envs")
	if dryRun {
		envDir = filepath.Join(".kdc", "envs")
	}
	if err := envfiles.Write(reg, envDir); err != nil {
		return fmt.Errorf("write env files: %w", err)
	}

	// 6. Write compose output.
	dest := outputPath
	if dryRun {
		dest = "-"
	}
	if err := compose.Write(project, dest); err != nil {
		return err
	}

	if dest != "-" {
		fmt.Fprintf(os.Stderr, "wrote %s\n", dest)
		fmt.Fprintf(os.Stderr, "wrote env files to %s/\n", envDir)
	}
	return nil
}
