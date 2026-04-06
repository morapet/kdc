package translator

import (
	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
)

// Translator orchestrates the two-pass translation strategy.
type Translator struct {
	reg *registry.ResourceRegistry
	ctx kdctypes.TranslationContext
}

// New creates a Translator for the given registry and context.
func New(reg *registry.ResourceRegistry, ctx kdctypes.TranslationContext) *Translator {
	return &Translator{reg: reg, ctx: ctx}
}

// Translate builds a compose-go Project from all resources in the registry.
func (t *Translator) Translate() (*comptypes.Project, error) {
	project := &comptypes.Project{
		Name:     t.ctx.ProjectName,
		Services: comptypes.Services{},
		Volumes:  comptypes.Volumes{},
		Configs:  map[string]comptypes.ConfigObjConfig{},
		Secrets:  comptypes.Secrets{},
	}

	// --- Pass 1: build lookup indexes ---
	cmIndex := buildConfigMapEnvIndex(t.reg)
	secIndex := buildSecretEnvIndex(t.reg)

	// --- Pass 2: translate resources ---

	// PVCs → named volumes
	for _, pvc := range t.reg.PVCs {
		name, cfg := translatePVC(pvc)
		project.Volumes[name] = cfg
	}

	// Deployments → services
	for _, d := range t.reg.Deployments {
		svcs := translateDeployment(d, cmIndex, secIndex, t.ctx.Namespace)
		for _, svc := range svcs {
			project.Services[svc.Name] = svc
		}
	}

	// Standalone Pods → services
	for _, p := range t.reg.Pods {
		svcs := translatePod(p, cmIndex, secIndex, t.ctx.Namespace)
		for _, svc := range svcs {
			project.Services[svc.Name] = svc
		}
	}

	return project, nil
}
