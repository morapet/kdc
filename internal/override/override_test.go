package override

import (
	"os"
	"path/filepath"
	"testing"

	comptypes "github.com/compose-spec/compose-go/v2/types"
)

func TestApply_PortsAppended(t *testing.T) {
	base := &comptypes.Project{
		Name: "test",
		Services: comptypes.Services{
			"web": {
				Name:  "web",
				Image: "nginx",
				Ports: []comptypes.ServicePortConfig{
					{Target: 80, Published: "8080"},
				},
				Environment: comptypes.MappingWithEquals{},
			},
		},
	}

	overrideYAML := `
services:
  web:
    ports:
      - target: 443
        published: "8443"
    environment:
      APP_ENV: development
`

	f := filepath.Join(t.TempDir(), "overrides.yaml")
	if err := os.WriteFile(f, []byte(overrideYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(base, f)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	svc := result.Services["web"]
	if len(svc.Ports) != 2 {
		t.Errorf("expected 2 ports, got %d: %+v", len(svc.Ports), svc.Ports)
	}

	// Check generated port preserved.
	foundGenerated := false
	foundOverride := false
	for _, p := range svc.Ports {
		if p.Target == 80 && p.Published == "8080" {
			foundGenerated = true
		}
		if p.Target == 443 && p.Published == "8443" {
			foundOverride = true
		}
	}
	if !foundGenerated {
		t.Error("generated port 80->8080 was lost after merge")
	}
	if !foundOverride {
		t.Error("override port 443->8443 was not added")
	}

	// Check env override wins.
	if svc.Environment["APP_ENV"] == nil || *svc.Environment["APP_ENV"] != "development" {
		t.Errorf("expected APP_ENV=development, got %v", svc.Environment["APP_ENV"])
	}
}

func TestApply_EmptyOverridePath(t *testing.T) {
	base := &comptypes.Project{Name: "test"}
	result, err := Apply(base, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != base {
		t.Error("expected same pointer returned when overridePath is empty")
	}
}

func TestApply_MissingFile(t *testing.T) {
	base := &comptypes.Project{Name: "test"}
	_, err := Apply(base, "/nonexistent/path/overrides.yaml")
	if err == nil {
		t.Error("expected error for missing override file")
	}
}
