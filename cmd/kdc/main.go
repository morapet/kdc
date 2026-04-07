package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/morapet/kdc/internal/compose"
	"github.com/morapet/kdc/internal/envfiles"
	"github.com/morapet/kdc/internal/filter"
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
		filtersPath   string
		projectName   string
		namespace     string
		verbose       bool
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate docker-compose.yaml from a Kustomize overlay",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(generateOpts{
				kustomizePath: kustomizePath,
				outputPath:    outputPath,
				overridePath:  overridePath,
				filtersPath:   filtersPath,
				projectName:   projectName,
				namespace:     namespace,
				verbose:       verbose,
				dryRun:        dryRun,
			})
		},
	}

	cmd.Flags().StringVarP(&kustomizePath, "kustomize", "k", "", "Path passed to `kustomize build` (required)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "docker-compose.yaml", "Output path (use \"-\" for stdout)")
	cmd.Flags().StringVar(&overridePath, "overrides", "", "Optional kdc-overrides.yaml for compose-level overrides")
	cmd.Flags().StringVar(&filtersPath, "filters", "", "Optional kdc-filters.yaml to skip or replace containers/resources")
	cmd.Flags().StringVar(&projectName, "project", "", "Compose project name (default: overlay dir basename)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Kubernetes namespace for resource lookups")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print filter messages and per-resource warnings to stderr")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print YAML to stdout, do not write file")

	_ = cmd.MarkFlagRequired("kustomize")
	return cmd
}

type generateOpts struct {
	kustomizePath string
	outputPath    string
	overridePath  string
	filtersPath   string
	projectName   string
	namespace     string
	verbose       bool
	dryRun        bool
}

func runGenerate(opts generateOpts) error {
	// Resolve project name.
	if opts.projectName == "" {
		abs, err := filepath.Abs(opts.kustomizePath)
		if err == nil {
			opts.projectName = filepath.Base(abs)
		} else {
			opts.projectName = filepath.Base(opts.kustomizePath)
		}
	}

	// 1. Run kustomize build.
	rawYAML, err := kustomize.Build(opts.kustomizePath)
	if err != nil {
		return err
	}

	// 2. Parse multi-document YAML into registry.
	reg, warnings, err := parser.Parse(rawYAML)
	if err != nil {
		return fmt.Errorf("parse kustomize output: %w", err)
	}

	// 3. Load filter config (optional) — must happen before warning reporting so
	// that resource kinds explicitly listed in resources.skip are silenced.
	var eng *filter.Engine
	if opts.filtersPath != "" {
		cfg, err := filter.Load(opts.filtersPath)
		if err != nil {
			return err
		}
		eng = filter.New(cfg)
	} else {
		eng = filter.New(nil)
	}

	// Report parse-time warnings for unknown resource kinds, suppressing any that
	// the user has explicitly acknowledged via resources.skip in their filter file.
	warnings = eng.SuppressKnownWarnings(warnings)
	if len(warnings) > 0 {
		if opts.verbose {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", w)
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: %d unsupported resource type(s) were skipped. Use --verbose for details.\n", len(warnings))
		}
	}

	// Require at least one workload.
	if len(reg.Deployments) == 0 && len(reg.Pods) == 0 {
		return fmt.Errorf("no translatable workload resources (Deployment or Pod) found in kustomize output")
	}

	// 4. Translate to compose Project.
	ctx := kdctypes.TranslationContext{
		Namespace:   opts.namespace,
		ProjectName: opts.projectName,
	}
	result, err := translator.New(reg, ctx, eng).Translate()
	if err != nil {
		return fmt.Errorf("translate: %w", err)
	}

	// Print translation messages (filter actions, injections, etc.).
	if opts.verbose {
		for _, msg := range result.Messages {
			fmt.Fprintf(os.Stderr, "info: %s\n", msg)
		}
	}

	// 5. Apply compose-level overrides.
	project, err := override.Apply(result.Project, opts.overridePath)
	if err != nil {
		return err
	}

	// 6. Write .env files for ConfigMaps and Secrets (envFrom references).
	var envDir string
	if !opts.dryRun {
		envDir = filepath.Join(filepath.Dir(opts.outputPath), ".kdc", "envs")
		if err := envfiles.Write(reg, envDir); err != nil {
			return fmt.Errorf("write env files: %w", err)
		}
	}

	// 7. Write compose output.
	dest := opts.outputPath
	if opts.dryRun {
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
