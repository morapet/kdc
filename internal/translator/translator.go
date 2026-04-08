package translator

import (
	"fmt"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/filter"
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
)

// Translator orchestrates the two-pass translation strategy.
type Translator struct {
	reg    *registry.ResourceRegistry
	ctx    kdctypes.TranslationContext
	engine *filter.Engine
}

// New creates a Translator for the given registry, context, and optional filter
// engine. If engine is nil, a pass-through (no-op) engine is used.
func New(reg *registry.ResourceRegistry, ctx kdctypes.TranslationContext, eng *filter.Engine) *Translator {
	if eng == nil {
		eng = filter.New(nil)
	}
	return &Translator{reg: reg, ctx: ctx, engine: eng}
}

// TranslateResult is returned by Translate and carries the compose project and
// any informational messages produced during translation (e.g. skipped resources).
type TranslateResult struct {
	Project  *comptypes.Project
	Messages []string
}

// Translate builds a compose-go Project from all resources in the registry.
func (t *Translator) Translate() (*TranslateResult, error) {
	project := &comptypes.Project{
		Name:     t.ctx.ProjectName,
		Services: comptypes.Services{},
		Volumes:  comptypes.Volumes{},
		Configs:  map[string]comptypes.ConfigObjConfig{},
		Secrets:  comptypes.Secrets{},
	}
	var messages []string

	// --- Pass 1: build lookup indexes ---
	cmIndex := buildConfigMapEnvIndex(t.reg)
	secIndex := buildSecretEnvIndex(t.reg)

	// --- Pass 2: translate resources ---

	// PVCs → named volumes (resource-level filter applied)
	for _, pvc := range t.reg.PVCs {
		if skip, reason := t.engine.ShouldSkipResource("PersistentVolumeClaim", pvc.Name); skip {
			messages = append(messages, fmt.Sprintf("skipped PVC %q: %s", pvc.Name, reason))
			continue
		}
		name, cfg := translatePVC(pvc)
		project.Volumes[name] = cfg
	}

	// injected tracks replacement services already added (dedup by service name).
	injected := map[string]bool{}

	// Deployments → services
	for _, d := range t.reg.Deployments {
		if skip, reason := t.engine.ShouldSkipResource("Deployment", d.Name); skip {
			messages = append(messages, fmt.Sprintf("skipped Deployment %q: %s", d.Name, reason))
			continue
		}
		svcs, replacements, msgs, err := translateDeployment(d, cmIndex, secIndex, t.ctx.Namespace, t.engine)
		messages = append(messages, msgs...)
		if err != nil {
			return nil, fmt.Errorf("translating Deployment %q: %w", d.Name, err)
		}
		for _, svc := range svcs {
			project.Services[svc.Name] = svc
		}
		for _, r := range replacements {
			if !injected[r.Name] {
				project.Services[r.Name] = r
				injected[r.Name] = true
				messages = append(messages, fmt.Sprintf("injected replacement service %q", r.Name))
			}
		}
	}

	// StatefulSets → services
	for _, s := range t.reg.StatefulSets {
		if skip, reason := t.engine.ShouldSkipResource("StatefulSet", s.Name); skip {
			messages = append(messages, fmt.Sprintf("skipped StatefulSet %q: %s", s.Name, reason))
			continue
		}
		svcs, replacements, msgs, err := translateStatefulSet(s, cmIndex, secIndex, t.ctx.Namespace, t.engine)
		messages = append(messages, msgs...)
		if err != nil {
			return nil, fmt.Errorf("translating StatefulSet %q: %w", s.Name, err)
		}
		for _, svc := range svcs {
			project.Services[svc.Name] = svc
		}
		for _, r := range replacements {
			if !injected[r.Name] {
				project.Services[r.Name] = r
				injected[r.Name] = true
				messages = append(messages, fmt.Sprintf("injected replacement service %q", r.Name))
			}
		}
	}

	// Standalone Pods → services
	for _, p := range t.reg.Pods {
		if skip, reason := t.engine.ShouldSkipResource("Pod", p.Name); skip {
			messages = append(messages, fmt.Sprintf("skipped Pod %q: %s", p.Name, reason))
			continue
		}
		svcs, replacements, msgs, err := translatePod(p, cmIndex, secIndex, t.ctx.Namespace, t.engine)
		messages = append(messages, msgs...)
		if err != nil {
			return nil, fmt.Errorf("translating Pod %q: %w", p.Name, err)
		}
		for _, svc := range svcs {
			project.Services[svc.Name] = svc
		}
		for _, r := range replacements {
			if !injected[r.Name] {
				project.Services[r.Name] = r
				injected[r.Name] = true
				messages = append(messages, fmt.Sprintf("injected replacement service %q", r.Name))
			}
		}
	}

	// K8s Services → network aliases on matching compose services.
	applyServiceAliases(project.Services, t.reg)

	return &TranslateResult{Project: project, Messages: messages}, nil
}
