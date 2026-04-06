package translator

import (
	"testing"
	"time"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestTranslateProbe_HTTP(t *testing.T) {
	p := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromInt32(8080),
			},
		},
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
		InitialDelaySeconds: 5,
		FailureThreshold:    3,
	}

	hc := translateProbe(p)
	if hc == nil {
		t.Fatal("expected non-nil HealthCheckConfig")
	}
	if len(hc.Test) < 2 {
		t.Fatalf("expected test slice len>=2, got %v", hc.Test)
	}
	if hc.Test[0] != "CMD-SHELL" {
		t.Errorf("expected CMD-SHELL, got %q", hc.Test[0])
	}
	if hc.Test[1] != "curl -sf http://localhost:8080/health" {
		t.Errorf("unexpected test command: %q", hc.Test[1])
	}

	wantInterval := comptypes.Duration(10 * time.Second)
	if *hc.Interval != wantInterval {
		t.Errorf("interval mismatch: want %v, got %v", wantInterval, *hc.Interval)
	}
}

func TestTranslateProbe_Exec(t *testing.T) {
	p := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"sh", "-c", "test -f /ready"}},
		},
		PeriodSeconds: 5,
	}
	hc := translateProbe(p)
	if hc == nil {
		t.Fatal("expected non-nil HealthCheckConfig")
	}
	if hc.Test[0] != "CMD-SHELL" || hc.Test[1] != "sh" {
		t.Errorf("unexpected exec test: %v", hc.Test)
	}
}

func TestTranslateProbe_TCP(t *testing.T) {
	p := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(5432)},
		},
	}
	hc := translateProbe(p)
	if hc == nil {
		t.Fatal("expected non-nil HealthCheckConfig")
	}
	if hc.Test[1] != "nc -z localhost 5432" {
		t.Errorf("unexpected tcp test: %q", hc.Test[1])
	}
}

func TestTranslateResources(t *testing.T) {
	req := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}

	res := translateResources(req)

	if res.Limits == nil {
		t.Fatal("expected non-nil Limits")
	}
	// 500m = 0.5 cores
	if res.Limits.NanoCPUs != 0.5 {
		t.Errorf("expected 0.5 NanoCPUs limit, got %v", res.Limits.NanoCPUs)
	}
	// 256Mi = 268435456 bytes
	if res.Limits.MemoryBytes != 268435456 {
		t.Errorf("expected 268435456 bytes limit, got %v", res.Limits.MemoryBytes)
	}

	if res.Reservations == nil {
		t.Fatal("expected non-nil Reservations")
	}
	if res.Reservations.NanoCPUs != 0.1 {
		t.Errorf("expected 0.1 NanoCPUs reservation, got %v", res.Reservations.NanoCPUs)
	}
}

func TestEnvFromConfigMap_UsesEnvFile(t *testing.T) {
	cmIndex := map[string]map[string]string{
		"default/app-config": {"LOG_LEVEL": "debug", "MAX_CONN": "50"},
	}
	secIndex := map[string]map[string]string{}

	c := corev1.Container{
		Name:  "app",
		Image: "myapp:latest",
		EnvFrom: []corev1.EnvFromSource{
			{ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
			}},
		},
	}

	spec := corev1.PodSpec{Containers: []corev1.Container{c}}
	svcs := translatePodSpec("myapp", "default", spec, cmIndex, secIndex, "default", nil)
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	svc := svcs[0]

	// envFrom should produce an env_file: reference, NOT inline environment: entries.
	if len(svc.EnvFiles) != 1 {
		t.Fatalf("expected 1 env_file entry, got %d: %+v", len(svc.EnvFiles), svc.EnvFiles)
	}
	if svc.EnvFiles[0].Path != ".kdc/envs/app-config.env" {
		t.Errorf("unexpected env_file path: %q", svc.EnvFiles[0].Path)
	}
	if !svc.EnvFiles[0].Required {
		t.Error("expected env_file to be required")
	}

	// Keys from the ConfigMap should NOT be inlined into environment:.
	if svc.Environment["LOG_LEVEL"] != nil {
		t.Error("LOG_LEVEL should not be inlined when envFrom uses env_file")
	}
}

func TestEnvValueFrom_StillInlined(t *testing.T) {
	cmIndex := map[string]map[string]string{
		"default/app-config": {"LOG_LEVEL": "debug"},
	}
	secIndex := map[string]map[string]string{
		"default/db-secret": {"password": "s3cr3t"},
	}

	c := corev1.Container{
		Name:  "app",
		Image: "myapp:latest",
		Env: []corev1.EnvVar{
			{
				Name: "LOG_LEVEL",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
						Key:                  "LOG_LEVEL",
					},
				},
			},
			{
				Name: "DB_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"},
						Key:                  "password",
					},
				},
			},
		},
	}

	spec := corev1.PodSpec{Containers: []corev1.Container{c}}
	svcs := translatePodSpec("myapp", "default", spec, cmIndex, secIndex, "default", nil)
	svc := svcs[0]

	// Single-key valueFrom references should still be inlined.
	if svc.Environment["LOG_LEVEL"] == nil || *svc.Environment["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL=debug inlined, got %v", svc.Environment["LOG_LEVEL"])
	}
	if svc.Environment["DB_PASSWORD"] == nil || *svc.Environment["DB_PASSWORD"] != "s3cr3t" {
		t.Errorf("expected DB_PASSWORD=s3cr3t inlined, got %v", svc.Environment["DB_PASSWORD"])
	}
}
