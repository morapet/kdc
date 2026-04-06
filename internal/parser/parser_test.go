package parser

import (
	"testing"
)

const multiDocFixture = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:latest
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
data:
  key: value
---
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: default
type: Opaque
data:
  password: c2VjcmV0  # "secret" base64-encoded
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
  namespace: default
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-ingress
  namespace: default
spec: {}
`

func TestParse_BasicDispatch(t *testing.T) {
	reg, warnings, err := Parse([]byte(multiDocFixture))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(reg.Deployments) != 1 {
		t.Errorf("expected 1 Deployment, got %d", len(reg.Deployments))
	}
	if reg.Deployments[0].Name != "web" {
		t.Errorf("expected Deployment name 'web', got %q", reg.Deployments[0].Name)
	}

	if len(reg.ConfigMaps) != 1 {
		t.Errorf("expected 1 ConfigMap, got %d", len(reg.ConfigMaps))
	}
	cm := reg.ConfigMap("default", "app-config")
	if cm == nil {
		t.Fatal("ConfigMap 'app-config' not found")
	}
	if cm.Data["key"] != "value" {
		t.Errorf("expected ConfigMap data key='value', got %q", cm.Data["key"])
	}

	if len(reg.Secrets) != 1 {
		t.Errorf("expected 1 Secret, got %d", len(reg.Secrets))
	}
	sec := reg.Secret("default", "my-secret")
	if sec == nil {
		t.Fatal("Secret 'my-secret' not found")
	}
	if string(sec.Data["password"]) != "secret" {
		t.Errorf("expected Secret data password='secret', got %q", string(sec.Data["password"]))
	}

	if len(reg.PVCs) != 1 {
		t.Errorf("expected 1 PVC, got %d", len(reg.PVCs))
	}
	if reg.PVCs[0].Name != "my-pvc" {
		t.Errorf("expected PVC name 'my-pvc', got %q", reg.PVCs[0].Name)
	}

	// Ingress should produce a warning, not an error.
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for Ingress, got %d", len(warnings))
	}
	if warnings[0].Kind != "Ingress" {
		t.Errorf("expected warning kind 'Ingress', got %q", warnings[0].Kind)
	}
}

func TestParse_Empty(t *testing.T) {
	reg, warnings, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Deployments) != 0 || len(reg.ConfigMaps) != 0 {
		t.Error("expected empty registry for empty input")
	}
	if len(warnings) != 0 {
		t.Error("expected no warnings for empty input")
	}
}
