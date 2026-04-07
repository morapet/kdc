package filter

import (
	"os"
	"path/filepath"
	"testing"

	kdctypes "github.com/morapet/kdc/pkg/types"
)

// --- glob matching -----------------------------------------------------------

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Exact matches
		{"istio-proxy", "istio-proxy", true},
		{"istio-proxy", "istio-PROXY", false},
		{"istio-proxy", "other", false},

		// Star wildcard (crosses /)
		{"*proxy*", "istio-proxy", true},
		{"gcr.io/*/gcsfuse*", "gcr.io/myproject/gcsfuse:latest", true},
		{"gcr.io/*/gcsfuse*", "gcr.io/myproject/other:latest", false},
		{"*", "anything/with/slashes", true},
		{"prefix-*", "prefix-foo", true},
		{"prefix-*", "notprefix-foo", false},

		// Question mark wildcard
		{"foo-?", "foo-1", true},
		{"foo-?", "foo-12", false},

		// No wildcards — exact
		{"plain", "plain", true},
		{"plain", "plain2", false},
	}

	for _, tc := range cases {
		got := globMatch(tc.pattern, tc.input)
		if got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.input, got, tc.want)
		}
	}
}

// --- container skip ----------------------------------------------------------

func TestEngine_ShouldSkipContainer(t *testing.T) {
	eng := New(&Config{
		Containers: ContainerFilters{
			Skip: []ContainerMatcher{
				{Name: "istio-proxy"},
				{Image: "gcr.io/*/cloud-sql-proxy*"},
			},
		},
	})

	cases := []struct {
		name, image string
		wantSkip    bool
	}{
		{"istio-proxy", "docker.io/istio/proxyv2:1.20", true},
		{"my-app", "myrepo/app:latest", false},
		{"cloud-sql-proxy", "gcr.io/cloud-sql-connectors/cloud-sql-proxy:2.0", true},
		{"cloud-sql-proxy", "other.io/cloud-sql-proxy:2.0", false}, // image doesn't match gcr.io glob
	}

	for _, tc := range cases {
		skip, reason := eng.ShouldSkipContainer(tc.name, tc.image)
		if skip != tc.wantSkip {
			t.Errorf("ShouldSkipContainer(%q, %q) skip=%v (reason=%q), want skip=%v",
				tc.name, tc.image, skip, reason, tc.wantSkip)
		}
	}
}

// --- container replace -------------------------------------------------------

func TestEngine_FindReplacement(t *testing.T) {
	eng := New(&Config{
		Containers: ContainerFilters{
			Replace: []ContainerReplacement{
				{
					Match: ContainerMatcher{Name: "cloud-sql-proxy"},
					With: ReplaceService{
						Name:  "postgres",
						Image: "postgres:16",
					},
				},
			},
		},
	})

	if r := eng.FindReplacement("cloud-sql-proxy", "gcr.io/cloud-sql-proxy:2"); r == nil {
		t.Error("expected replacement for cloud-sql-proxy, got nil")
	} else if r.With.Name != "postgres" {
		t.Errorf("expected replacement service name 'postgres', got %q", r.With.Name)
	}

	if r := eng.FindReplacement("other-container", "myimage:latest"); r != nil {
		t.Errorf("unexpected replacement for other-container: %+v", r)
	}
}

// --- resource skip -----------------------------------------------------------

func TestEngine_ShouldSkipResource(t *testing.T) {
	eng := New(&Config{
		Resources: ResourceFilters{
			Skip: []ResourceMatcher{
				{Kind: "HorizontalPodAutoscaler"},
				{Kind: "Ingress"},
				{Name: "legacy-*"},
			},
		},
	})

	cases := []struct {
		kind, name string
		wantSkip   bool
	}{
		{"HorizontalPodAutoscaler", "my-hpa", true},
		{"Ingress", "web-ingress", true},
		{"Deployment", "legacy-app", true},    // matched by name glob, any kind
		{"Deployment", "modern-app", false},
		{"ConfigMap", "legacy-config", true},  // name glob matches regardless of kind
		{"Service", "web", false},
	}

	for _, tc := range cases {
		skip, reason := eng.ShouldSkipResource(tc.kind, tc.name)
		if skip != tc.wantSkip {
			t.Errorf("ShouldSkipResource(%q, %q) skip=%v (reason=%q), want %v",
				tc.kind, tc.name, skip, reason, tc.wantSkip)
		}
	}
}

// --- no-op engine (nil config) -----------------------------------------------

func TestEngine_NoOp(t *testing.T) {
	eng := New(nil)

	if skip, _ := eng.ShouldSkipContainer("anything", "image"); skip {
		t.Error("nil config engine should not skip containers")
	}
	if r := eng.FindReplacement("anything", "image"); r != nil {
		t.Error("nil config engine should not produce replacements")
	}
	if skip, _ := eng.ShouldSkipResource("Deployment", "my-app"); skip {
		t.Error("nil config engine should not skip resources")
	}
}

// --- Load from file ----------------------------------------------------------

func TestLoad(t *testing.T) {
	content := `
containers:
  skip:
    - name: istio-proxy
  replace:
    - match:
        name: cloud-sql-proxy
      with:
        name: postgres
        image: postgres:16
        environment:
          POSTGRES_PASSWORD: postgres
resources:
  skip:
    - kind: HorizontalPodAutoscaler
`
	f := filepath.Join(t.TempDir(), "filters.yaml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.Containers.Skip) != 1 || cfg.Containers.Skip[0].Name != "istio-proxy" {
		t.Errorf("unexpected containers.skip: %+v", cfg.Containers.Skip)
	}
	if len(cfg.Containers.Replace) != 1 || cfg.Containers.Replace[0].With.Image != "postgres:16" {
		t.Errorf("unexpected containers.replace: %+v", cfg.Containers.Replace)
	}
	if len(cfg.Resources.Skip) != 1 || cfg.Resources.Skip[0].Kind != "HorizontalPodAutoscaler" {
		t.Errorf("unexpected resources.skip: %+v", cfg.Resources.Skip)
	}
}

// --- SuppressKnownWarnings ---------------------------------------------------

func TestSuppressKnownWarnings(t *testing.T) {
	eng := New(&Config{
		Resources: ResourceFilters{
			Skip: []ResourceMatcher{
				{Kind: "DestinationRule"},
				{Kind: "VirtualService"},
				{Kind: "NetworkPolicy"},
			},
		},
	})

	warnings := []kdctypes.UnsupportedResourceWarning{
		{APIVersion: "networking.istio.io/v1beta1", Kind: "DestinationRule", Name: "web-dr"},
		{APIVersion: "networking.istio.io/v1beta1", Kind: "VirtualService", Name: "web-vs"},
		{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy", Name: "deny-all"},
		{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler", Name: "web-hpa"}, // NOT in skip
		{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "redis"},                       // NOT in skip
	}

	remaining := eng.SuppressKnownWarnings(warnings)

	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining warnings, got %d: %v", len(remaining), remaining)
	}
	for _, w := range remaining {
		if w.Kind == "DestinationRule" || w.Kind == "VirtualService" || w.Kind == "NetworkPolicy" {
			t.Errorf("kind %q should have been suppressed", w.Kind)
		}
	}
}

func TestSuppressKnownWarnings_NoOp(t *testing.T) {
	eng := New(nil)
	warnings := []kdctypes.UnsupportedResourceWarning{
		{Kind: "DestinationRule", Name: "dr"},
	}
	remaining := eng.SuppressKnownWarnings(warnings)
	if len(remaining) != 1 {
		t.Errorf("nil engine should not suppress any warnings, got %d", len(remaining))
	}
}

// --- both Name+Image must match when both are set ----------------------------

func TestContainerMatcher_BothRequired(t *testing.T) {
	eng := New(&Config{
		Containers: ContainerFilters{
			Skip: []ContainerMatcher{
				{Name: "sidecar", Image: "gcr.io/*"},
			},
		},
	})

	// Only name matches → no skip
	if skip, _ := eng.ShouldSkipContainer("sidecar", "docker.io/other:latest"); skip {
		t.Error("should not skip: image does not match gcr.io/*")
	}
	// Both match → skip
	if skip, _ := eng.ShouldSkipContainer("sidecar", "gcr.io/myproj/sidecar:v1"); !skip {
		t.Error("should skip: both name and image match")
	}
}
