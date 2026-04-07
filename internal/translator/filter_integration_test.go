package translator

import (
	"testing"

	"github.com/morapet/kdc/internal/filter"
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// deployment with three containers: main app + istio-proxy sidecar + cloud-sql-proxy
func makeFilterFixtureDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "migrate", Image: "flyway:9"},
					},
					Containers: []corev1.Container{
						{Name: "web", Image: "nginx:1.25"},
						{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:1.20"},
						{Name: "cloud-sql-proxy", Image: "gcr.io/cloud-sql-connectors/cloud-sql-proxy:2.6"},
					},
				},
			},
		},
	}
}

func TestTranslate_FilterSkipsIstioAndReplacesCloudSQL(t *testing.T) {
	eng := filter.New(&filter.Config{
		Containers: filter.ContainerFilters{
			Skip: []filter.ContainerMatcher{
				{Name: "istio-proxy"},
			},
			Replace: []filter.ContainerReplacement{
				{
					Match: filter.ContainerMatcher{Name: "cloud-sql-proxy"},
					With: filter.ReplaceService{
						Name:  "postgres",
						Image: "postgres:16-alpine",
						Environment: map[string]string{
							"POSTGRES_PASSWORD": "postgres",
						},
					},
				},
			},
		},
	})

	reg := registry.New()
	reg.Deployments = append(reg.Deployments, makeFilterFixtureDeployment())

	ctx := kdctypes.TranslationContext{Namespace: "default", ProjectName: "test"}
	result, err := New(reg, ctx, eng).Translate()
	if err != nil {
		t.Fatalf("Translate error: %v", err)
	}
	p := result.Project

	// The main "web" service should be present.
	if _, ok := p.Services["web"]; !ok {
		t.Error("expected 'web' service in output")
	}

	// The istio-proxy sidecar should be absent.
	if _, ok := p.Services["web-istio-proxy"]; ok {
		t.Error("istio-proxy sidecar should have been skipped")
	}

	// The cloud-sql-proxy should be absent (it was replaced).
	if _, ok := p.Services["web-cloud-sql-proxy"]; ok {
		t.Error("cloud-sql-proxy should have been replaced, not translated")
	}

	// The postgres replacement should be present.
	pg, ok := p.Services["postgres"]
	if !ok {
		t.Fatal("expected injected 'postgres' service")
	}
	if pg.Image != "postgres:16-alpine" {
		t.Errorf("expected postgres image 'postgres:16-alpine', got %q", pg.Image)
	}
	if pg.Environment["POSTGRES_PASSWORD"] == nil || *pg.Environment["POSTGRES_PASSWORD"] != "postgres" {
		t.Error("expected POSTGRES_PASSWORD=postgres on injected postgres service")
	}

	// Messages should document what happened.
	foundSkip := false
	foundReplace := false
	for _, msg := range result.Messages {
		if containsAll(msg, "skipped", "istio-proxy") {
			foundSkip = true
		}
		if containsAll(msg, "replaced", "cloud-sql-proxy", "postgres") {
			foundReplace = true
		}
	}
	if !foundSkip {
		t.Errorf("expected skip message for istio-proxy, messages: %v", result.Messages)
	}
	if !foundReplace {
		t.Errorf("expected replace message for cloud-sql-proxy, messages: %v", result.Messages)
	}
}

func TestTranslate_FilterSkipsResource(t *testing.T) {
	eng := filter.New(&filter.Config{
		Resources: filter.ResourceFilters{
			Skip: []filter.ResourceMatcher{
				{Kind: "Deployment", Name: "legacy-*"},
			},
		},
	})

	reg := registry.New()
	reg.Deployments = append(reg.Deployments,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "legacy-app", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "old:1"}},
				},
			}},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "modern-app", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "new:1"}},
				},
			}},
		},
	)

	ctx := kdctypes.TranslationContext{Namespace: "default", ProjectName: "test"}
	result, err := New(reg, ctx, eng).Translate()
	if err != nil {
		t.Fatalf("Translate error: %v", err)
	}

	if _, ok := result.Project.Services["legacy-app"]; ok {
		t.Error("legacy-app should have been skipped by resource filter")
	}
	if _, ok := result.Project.Services["modern-app"]; !ok {
		t.Error("modern-app should be present (not matched by filter)")
	}
}

func TestTranslate_ReplacementDeduplication(t *testing.T) {
	// Two deployments both have cloud-sql-proxy; only one postgres should be injected.
	eng := filter.New(&filter.Config{
		Containers: filter.ContainerFilters{
			Replace: []filter.ContainerReplacement{
				{
					Match: filter.ContainerMatcher{Name: "cloud-sql-proxy"},
					With:  filter.ReplaceService{Name: "postgres", Image: "postgres:16"},
				},
			},
		},
	})

	reg := registry.New()
	for _, name := range []string{"api", "worker"} {
		reg.Deployments = append(reg.Deployments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: name, Image: "myapp:1"},
						{Name: "cloud-sql-proxy", Image: "gcr.io/cloud-sql-proxy:2"},
					},
				},
			}},
		})
	}

	ctx := kdctypes.TranslationContext{Namespace: "default", ProjectName: "test"}
	result, err := New(reg, ctx, eng).Translate()
	if err != nil {
		t.Fatalf("Translate error: %v", err)
	}

	// Count postgres services — must be exactly 1.
	count := 0
	for name := range result.Project.Services {
		if name == "postgres" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 postgres service, got %d", count)
	}
}

// containsAll returns true if s contains all of the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
