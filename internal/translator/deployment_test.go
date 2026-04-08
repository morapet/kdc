package translator

import (
	"testing"
	"time"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/filter"
	"github.com/morapet/kdc/internal/registry"
	kdctypes "github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if hc.Test[1] != "bash -c '(echo >/dev/tcp/localhost/8080) 2>/dev/null'" {
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
	if hc.Test[0] != "CMD" || hc.Test[1] != "sh" {
		t.Errorf("unexpected exec test: %v", hc.Test)
	}
	// Full command should be ["CMD", "sh", "-c", "test -f /ready"]
	if len(hc.Test) != 4 {
		t.Errorf("expected 4 elements, got %d: %v", len(hc.Test), hc.Test)
	}
}

func TestTranslatePodSpec_SourceKindLabel(t *testing.T) {
	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{Name: "app", Image: "myapp:latest"},
		},
	}

	// Deployment kind
	svcs, _, _, err := translatePodSpec("Deployment", "myapp", "default", spec, nil, nil, "default", nil, filter.New(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if got := svcs[0].Labels["kdc.io/source-kind"]; got != "Deployment" {
		t.Errorf("expected Deployment label, got %q", got)
	}

	// Pod kind
	svcs, _, _, err = translatePodSpec("Pod", "mypod", "default", spec, nil, nil, "default", nil, filter.New(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if got := svcs[0].Labels["kdc.io/source-kind"]; got != "Pod" {
		t.Errorf("expected Pod label, got %q", got)
	}
}

func TestTranslateContainerPorts(t *testing.T) {
	c := corev1.Container{
		Name:  "app",
		Image: "myapp:latest",
		Ports: []corev1.ContainerPort{
			{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
			{ContainerPort: 9090, Protocol: corev1.ProtocolUDP},
			{ContainerPort: 3000}, // no protocol — should default to tcp
		},
	}
	svc, err := translateContainer("app", "default", c, corev1.PodSpec{}, nil, nil, nil, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(svc.Ports) != 3 {
		t.Fatalf("expected 3 ports, got %d", len(svc.Ports))
	}

	if svc.Ports[0].Target != 8080 {
		t.Errorf("expected target 8080, got %d", svc.Ports[0].Target)
	}
	if svc.Ports[0].Published != "8080" {
		t.Errorf("expected published 8080, got %q", svc.Ports[0].Published)
	}
	if svc.Ports[0].Protocol != "tcp" {
		t.Errorf("expected protocol tcp, got %q", svc.Ports[0].Protocol)
	}

	if svc.Ports[1].Target != 9090 {
		t.Errorf("expected target 9090, got %d", svc.Ports[1].Target)
	}
	if svc.Ports[1].Protocol != "udp" {
		t.Errorf("expected protocol udp, got %q", svc.Ports[1].Protocol)
	}

	// Default protocol when empty
	if svc.Ports[2].Target != 3000 {
		t.Errorf("expected target 3000, got %d", svc.Ports[2].Target)
	}
	if svc.Ports[2].Protocol != "tcp" {
		t.Errorf("expected default protocol tcp, got %q", svc.Ports[2].Protocol)
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
	if hc.Test[1] != "bash -c '(echo >/dev/tcp/localhost/5432) 2>/dev/null'" {
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
	svcs, _, _, err := translatePodSpec("Pod", "myapp", "default", spec, cmIndex, secIndex, "default", nil, filter.New(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	svcs, _, _, err := translatePodSpec("Pod", "myapp", "default", spec, cmIndex, secIndex, "default", nil, filter.New(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := svcs[0]

	// Single-key valueFrom references should still be inlined.
	if svc.Environment["LOG_LEVEL"] == nil || *svc.Environment["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL=debug inlined, got %v", svc.Environment["LOG_LEVEL"])
	}
	if svc.Environment["DB_PASSWORD"] == nil || *svc.Environment["DB_PASSWORD"] != "s3cr3t" {
		t.Errorf("expected DB_PASSWORD=s3cr3t inlined, got %v", svc.Environment["DB_PASSWORD"])
	}
}

func TestTranslateStatefulSet_SourceKindLabel(t *testing.T) {
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "db", Image: "postgres:16"},
					},
				},
			},
		},
	}

	svcs, _, _, err := translateStatefulSet(ss, nil, nil, "default", filter.New(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if got := svcs[0].Labels[kdctypes.AnnotationSourceKind]; got != "StatefulSet" {
		t.Errorf("expected source-kind=StatefulSet, got %q", got)
	}
	if got := svcs[0].Labels[kdctypes.AnnotationSourceName]; got != "db" {
		t.Errorf("expected source-name=db, got %q", got)
	}
}

func TestApplyServiceAliases(t *testing.T) {
	reg := registry.New()

	// A Deployment with pod template labels app=web
	reg.Deployments = append(reg.Deployments, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "web"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "nginx:latest"}},
				},
			},
		},
	})

	// A K8s Service selecting app=web
	reg.AddService(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
		},
	})

	// Build a compose services map as the translator would
	services := comptypes.Services{
		"web": comptypes.ServiceConfig{
			Name:  "web",
			Image: "nginx:latest",
			Labels: comptypes.Labels{
				kdctypes.AnnotationSourceKind:      "Deployment",
				kdctypes.AnnotationSourceName:      "web",
				kdctypes.AnnotationSourceNamespace: "default",
			},
		},
	}

	applyServiceAliases(services, reg)

	webSvc := services["web"]
	if webSvc.Networks == nil {
		t.Fatal("expected networks to be set")
	}
	defaultNet, ok := webSvc.Networks["default"]
	if !ok {
		t.Fatal("expected 'default' network entry")
	}
	if len(defaultNet.Aliases) == 0 || defaultNet.Aliases[0] != "web-svc" {
		t.Errorf("expected alias 'web-svc', got %v", defaultNet.Aliases)
	}
}

// TestTranslateVolumeMounts_ConfigMapNoSubPath verifies that a ConfigMap mount
// without subPath produces a directory bind mount (existing behaviour).
func TestTranslateVolumeMounts_ConfigMapNoSubPath(t *testing.T) {
	volSources := map[string]volumeSource{
		"my-cm": {kind: "configMap", name: "my-configmap"},
	}
	mounts := []corev1.VolumeMount{
		{Name: "my-cm", MountPath: "/etc/config"},
	}

	vols, err := translateVolumeMounts(mounts, volSources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].Source != "./.kdc/configs/my-configmap" {
		t.Errorf("expected directory source, got %q", vols[0].Source)
	}
	if vols[0].Target != "/etc/config" {
		t.Errorf("expected target /etc/config, got %q", vols[0].Target)
	}
	if vols[0].Type != "bind" {
		t.Errorf("expected bind type, got %q", vols[0].Type)
	}
}

// TestTranslateVolumeMounts_ConfigMapWithSubPath verifies that a ConfigMap mount
// with subPath produces a file-level bind mount.
func TestTranslateVolumeMounts_ConfigMapWithSubPath(t *testing.T) {
	volSources := map[string]volumeSource{
		"init-scripts": {kind: "configMap", name: "pricing-database-init-2fkb264hk4"},
	}
	mounts := []corev1.VolumeMount{
		{
			Name:      "init-scripts",
			MountPath: "/docker-entrypoint-initdb.d/database-init.sh",
			SubPath:   "database-init.sh",
			ReadOnly:  true,
		},
	}

	vols, err := translateVolumeMounts(mounts, volSources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	want := "./.kdc/configs/pricing-database-init-2fkb264hk4/database-init.sh"
	if vols[0].Source != want {
		t.Errorf("expected source %q, got %q", want, vols[0].Source)
	}
	if vols[0].Target != "/docker-entrypoint-initdb.d/database-init.sh" {
		t.Errorf("unexpected target: %q", vols[0].Target)
	}
	if !vols[0].ReadOnly {
		t.Error("expected ReadOnly to be true")
	}
}

// TestTranslateVolumeMounts_SecretWithSubPath verifies that a Secret mount with
// subPath produces a file-level bind mount inside .kdc/secrets/.
func TestTranslateVolumeMounts_SecretWithSubPath(t *testing.T) {
	volSources := map[string]volumeSource{
		"tls-secret": {kind: "secret", name: "my-tls"},
	}
	mounts := []corev1.VolumeMount{
		{
			Name:      "tls-secret",
			MountPath: "/etc/ssl/certs/tls.crt",
			SubPath:   "tls.crt",
		},
	}

	vols, err := translateVolumeMounts(mounts, volSources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	want := "./.kdc/secrets/my-tls/tls.crt"
	if vols[0].Source != want {
		t.Errorf("expected source %q, got %q", want, vols[0].Source)
	}
	if vols[0].Target != "/etc/ssl/certs/tls.crt" {
		t.Errorf("unexpected target: %q", vols[0].Target)
	}
}

// TestTranslateVolumeMounts_UnsafeSubPath verifies that path-traversal subPaths
// are rejected with an error.
func TestTranslateVolumeMounts_UnsafeSubPath(t *testing.T) {
	volSources := map[string]volumeSource{
		"my-cm": {kind: "configMap", name: "my-configmap"},
	}

	cases := []struct {
		name    string
		subPath string
	}{
		{"dotdot", "../passwd"},
		{"absolute", "/etc/passwd"},
		{"dotdot-nested", "foo/../../../etc/passwd"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mounts := []corev1.VolumeMount{
				{Name: "my-cm", MountPath: "/target", SubPath: tc.subPath},
			}
			_, err := translateVolumeMounts(mounts, volSources)
			if err == nil {
				t.Errorf("expected error for unsafe subPath %q, got nil", tc.subPath)
			}
		})
	}
}

// TestTranslateVolumeMounts_SubPathExpr verifies that SubPathExpr mounts are
// silently skipped because compose cannot evaluate Kubernetes API expressions.
func TestTranslateVolumeMounts_SubPathExpr(t *testing.T) {
	volSources := map[string]volumeSource{
		"my-cm": {kind: "configMap", name: "my-configmap"},
	}
	mounts := []corev1.VolumeMount{
		{Name: "my-cm", MountPath: "/etc/config", SubPathExpr: "$(MY_VAR)"},
	}

	vols, err := translateVolumeMounts(mounts, volSources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 0 {
		t.Errorf("expected SubPathExpr mount to be skipped, got %d volumes", len(vols))
	}
}

// TestTranslateVolumeMounts_PVCAndEmptyDir verifies that PVC and emptyDir mounts
// are unaffected by the subPath change.
func TestTranslateVolumeMounts_PVCAndEmptyDir(t *testing.T) {
	volSources := map[string]volumeSource{
		"data-pvc":  {kind: "pvc", name: "postgres-data"},
		"tmp-cache": {kind: "emptyDir", name: "tmp-cache"},
	}
	mounts := []corev1.VolumeMount{
		{Name: "data-pvc", MountPath: "/var/lib/postgresql/data"},
		{Name: "tmp-cache", MountPath: "/tmp/cache"},
	}

	vols, err := translateVolumeMounts(mounts, volSources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}

	pvcVol := vols[0]
	if pvcVol.Type != "volume" {
		t.Errorf("expected PVC to produce 'volume' type, got %q", pvcVol.Type)
	}
	if pvcVol.Source != "postgres-data" {
		t.Errorf("unexpected PVC source: %q", pvcVol.Source)
	}

	emptyVol := vols[1]
	if emptyVol.Type != "tmpfs" {
		t.Errorf("expected emptyDir to produce 'tmpfs' type, got %q", emptyVol.Type)
	}
}
