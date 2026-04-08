package translator

import (
	"fmt"
	"strings"
	"time"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/filter"
	kdctypes "github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// escapeComposeVars replaces every `${` with `$${` in s so that Docker Compose
// does not interpolate shell variable references at render time. The container
// shell will expand them at runtime instead. This is needed when a K8s
// container command/args string contains variables like `${REDIS_PASSWORD}`
// that are supplied via env_file (applied at container start, not at compose
// render time).
func escapeComposeVars(s string) string {
	return strings.ReplaceAll(s, "${", "$${")
}

// escapeShellCommand applies escapeComposeVars to every element of args.
func escapeShellCommand(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = escapeComposeVars(a)
	}
	return out
}

// isSafeSubPath returns true when subPath is safe to use as a path component
// inside a .kdc directory. It rejects absolute paths and any path that
// contains ".." segments to prevent directory-traversal attacks.
func isSafeSubPath(subPath string) bool {
	if strings.HasPrefix(subPath, "/") {
		return false
	}
	for _, part := range strings.Split(subPath, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}

// translateDeployment converts a Deployment into compose services.
// Returns (translated services, injected replacement services, info messages).
func translateDeployment(
	d *appsv1.Deployment,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string, err error) {
	ns := d.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return translatePodSpec("Deployment", d.Name, ns, d.Spec.Template.Spec, cmIndex, secIndex, defaultNamespace, d.Labels, eng)
}

// translateStatefulSet converts a StatefulSet into compose services.
// Returns (translated services, injected replacement services, info messages).
func translateStatefulSet(
	s *appsv1.StatefulSet,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string, err error) {
	ns := s.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return translatePodSpec("StatefulSet", s.Name, ns, s.Spec.Template.Spec, cmIndex, secIndex, defaultNamespace, s.Labels, eng)
}

// translatePod converts a standalone Pod into compose services.
func translatePod(
	p *corev1.Pod,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string, err error) {
	ns := p.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return translatePodSpec("Pod", p.Name, ns, p.Spec, cmIndex, secIndex, defaultNamespace, p.Labels, eng)
}

func translatePodSpec(
	sourceKind string,
	ownerName string,
	namespace string,
	spec corev1.PodSpec,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	sourceLabels map[string]string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string, err error) {
	// Build a map of volume name -> source type for reference resolution.
	volSources := buildVolumeSources(spec.Volumes)

	// Track which replacement service names have already been queued (per pod-spec)
	// to avoid adding the same replacement twice from multi-container pods.
	replacementSeen := map[string]bool{}

	// Process init containers — they are not translated but we log skips.
	for _, ic := range spec.InitContainers {
		_, reason := eng.ShouldSkipInitContainer(ic.Name, ic.Image)
		messages = append(messages, fmt.Sprintf(
			"skipped init container %q (image=%s): %s", ic.Name, ic.Image, reason))
	}

	// Process regular containers.
	primary := true
	for _, c := range spec.Containers {
		// --- filter: skip ---
		if skip, reason := eng.ShouldSkipContainer(c.Name, c.Image); skip {
			messages = append(messages, fmt.Sprintf(
				"skipped container %q (image=%s): %s", c.Name, c.Image, reason))
			continue
		}

		// --- filter: replace ---
		if rep := eng.FindReplacement(c.Name, c.Image); rep != nil {
			messages = append(messages, fmt.Sprintf(
				"replaced container %q (image=%s) with service %q (image=%s)",
				c.Name, c.Image, rep.With.Name, rep.With.Image))
			if !replacementSeen[rep.With.Name] {
				replacementSeen[rep.With.Name] = true
				replacements = append(replacements, rep.With.ToServiceConfig())
			}
			continue
		}

		// --- normal translation ---
		name := ownerName
		if !primary {
			name = ownerName + "-" + c.Name
		}
		primary = false

		svc, translateErr := translateContainer(name, namespace, c, spec, volSources, cmIndex, secIndex, defaultNamespace)
		if translateErr != nil {
			return nil, nil, messages, translateErr
		}
		svc.Labels = comptypes.Labels{
			kdctypes.AnnotationSourceKind:      sourceKind,
			kdctypes.AnnotationSourceName:      ownerName,
			kdctypes.AnnotationSourceNamespace: namespace,
		}
		services = append(services, svc)
	}
	return services, replacements, messages, nil
}

// volumeSource describes what a pod Volume is backed by.
type volumeSource struct {
	kind string // "configMap", "secret", "pvc", "emptyDir", "other"
	name string // the configMap/secret/PVC name
}

func buildVolumeSources(volumes []corev1.Volume) map[string]volumeSource {
	m := make(map[string]volumeSource, len(volumes))
	for _, v := range volumes {
		switch {
		case v.ConfigMap != nil:
			m[v.Name] = volumeSource{kind: "configMap", name: v.ConfigMap.Name}
		case v.Secret != nil:
			m[v.Name] = volumeSource{kind: "secret", name: v.Secret.SecretName}
		case v.PersistentVolumeClaim != nil:
			m[v.Name] = volumeSource{kind: "pvc", name: v.PersistentVolumeClaim.ClaimName}
		case v.EmptyDir != nil:
			m[v.Name] = volumeSource{kind: "emptyDir", name: v.Name}
		default:
			m[v.Name] = volumeSource{kind: "other", name: v.Name}
		}
	}
	return m
}

func translateContainer(
	serviceName string,
	namespace string,
	c corev1.Container,
	spec corev1.PodSpec,
	volSources map[string]volumeSource,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
) (comptypes.ServiceConfig, error) {
	svc := comptypes.ServiceConfig{
		Name:        serviceName,
		Image:       c.Image,
		Environment: comptypes.MappingWithEquals{},
	}

	// Command / args: escape `${VAR}` → `$${VAR}` so Compose does not
	// interpolate shell variables at render time; the container shell expands
	// them at runtime.
	if len(c.Command) > 0 {
		svc.Entrypoint = comptypes.ShellCommand(escapeShellCommand(c.Command))
	}
	if len(c.Args) > 0 {
		svc.Command = comptypes.ShellCommand(escapeShellCommand(c.Args))
	}

	// Env vars: direct values first.
	for _, e := range c.Env {
		if e.Value != "" {
			val := e.Value
			svc.Environment[e.Name] = &val
			continue
		}
		if e.ValueFrom != nil {
			if ref := e.ValueFrom.ConfigMapKeyRef; ref != nil {
				key := configMapEnvKey(ref.Name, namespace)
				if kvs, ok := cmIndex[key]; ok {
					if v, ok := kvs[ref.Key]; ok {
						val := v
						svc.Environment[e.Name] = &val
					}
				}
			}
			if ref := e.ValueFrom.SecretKeyRef; ref != nil {
				key := secretEnvKey(ref.Name, namespace)
				if kvs, ok := secIndex[key]; ok {
					if v, ok := kvs[ref.Key]; ok {
						val := v
						svc.Environment[e.Name] = &val
					}
				}
			}
		}
	}

	// envFrom: reference the ConfigMap/Secret as an env_file instead of inlining
	// all key-value pairs into environment:. This creates a shared .env file that:
	//   - can be referenced by multiple services without duplication
	//   - allows variables within the file to reference each other
	//   - is editable by the developer without regenerating the compose file
	for _, ef := range c.EnvFrom {
		if ef.ConfigMapRef != nil {
			required := ef.ConfigMapRef.Optional == nil || !*ef.ConfigMapRef.Optional
			svc.EnvFiles = append(svc.EnvFiles, comptypes.EnvFile{
				Path:     fmt.Sprintf(".kdc/envs/%s.env", ef.ConfigMapRef.Name),
				Required: required,
			})
		}
		if ef.SecretRef != nil {
			required := ef.SecretRef.Optional == nil || !*ef.SecretRef.Optional
			svc.EnvFiles = append(svc.EnvFiles, comptypes.EnvFile{
				Path:     fmt.Sprintf(".kdc/envs/%s.env", ef.SecretRef.Name),
				Required: required,
			})
		}
	}

	// Volume mounts.
	vols, err := translateVolumeMounts(c.VolumeMounts, volSources)
	if err != nil {
		return comptypes.ServiceConfig{}, err
	}
	svc.Volumes = vols

	// Container ports.
	for _, p := range c.Ports {
		proto := strings.ToLower(string(p.Protocol))
		if proto == "" {
			proto = "tcp"
		}
		port := fmt.Sprintf("%d", p.ContainerPort)
		svc.Ports = append(svc.Ports, comptypes.ServicePortConfig{
			Target:    uint32(p.ContainerPort),
			Published: port,
			Protocol:  proto,
		})
	}

	// Health check (prefer ReadinessProbe over LivenessProbe).
	probe := c.ReadinessProbe
	if probe == nil {
		probe = c.LivenessProbe
	}
	if probe != nil {
		svc.HealthCheck = translateProbe(probe)
	}

	// Resource limits / reservations.
	if c.Resources.Limits != nil || c.Resources.Requests != nil {
		svc.Deploy = &comptypes.DeployConfig{
			Resources: translateResources(c.Resources),
		}
	}

	return svc, nil
}

// translateVolumeMounts converts container volume mounts to compose volume entries.
// ConfigMap and Secret mounts become compose configs/secrets references.
// PVC mounts become named volume references.
// EmptyDir mounts become anonymous volumes.
//
// When a mount carries a non-empty SubPath the bind source is narrowed to the
// specific file inside the .kdc directory so that Docker does not create a
// directory where the container expects a single file.  SubPathExpr is not
// supported because Docker Compose cannot evaluate Kubernetes API expressions;
// such mounts are silently skipped with no bind entry emitted.
//
// SubPath values that are absolute paths or contain ".." segments are rejected
// with an error to prevent directory-traversal attacks.
func translateVolumeMounts(mounts []corev1.VolumeMount, volSources map[string]volumeSource) ([]comptypes.ServiceVolumeConfig, error) {
	var vols []comptypes.ServiceVolumeConfig
	for _, m := range mounts {
		src, ok := volSources[m.Name]
		if !ok {
			continue
		}
		switch src.kind {
		case "configMap":
			source := fmt.Sprintf("./.kdc/configs/%s", src.name)
			if m.SubPathExpr != "" {
				// SubPathExpr uses the Downward API and cannot be evaluated at
				// compose-generation time; skip producing a bind entry.
				continue
			}
			if m.SubPath != "" {
				if !isSafeSubPath(m.SubPath) {
					return nil, fmt.Errorf("unsafe subPath %q in volume mount %q: must be a relative path without absolute prefix or '..' segments", m.SubPath, m.Name)
				}
				source = fmt.Sprintf("./.kdc/configs/%s/%s", src.name, m.SubPath)
			}
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:     "bind",
				Source:   source,
				Target:   m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "secret":
			source := fmt.Sprintf("./.kdc/secrets/%s", src.name)
			if m.SubPathExpr != "" {
				// SubPathExpr uses the Downward API and cannot be evaluated at
				// compose-generation time; skip producing a bind entry.
				continue
			}
			if m.SubPath != "" {
				if !isSafeSubPath(m.SubPath) {
					return nil, fmt.Errorf("unsafe subPath %q in volume mount %q: must be a relative path without absolute prefix or '..' segments", m.SubPath, m.Name)
				}
				source = fmt.Sprintf("./.kdc/secrets/%s/%s", src.name, m.SubPath)
			}
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:     "bind",
				Source:   source,
				Target:   m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "pvc":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:     "volume",
				Source:   src.name,
				Target:   m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "emptyDir":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:   "tmpfs",
				Target: m.MountPath,
			})
		}
	}
	return vols, nil
}

// translateProbe converts a K8s Probe to a compose HealthCheckConfig.
func translateProbe(p *corev1.Probe) *comptypes.HealthCheckConfig {
	var test []string

	switch {
	case p.Exec != nil:
		test = append([]string{"CMD"}, p.Exec.Command...)
	case p.HTTPGet != nil:
		port := p.HTTPGet.Port.String()
		// Use a TCP-level check via bash's /dev/tcp built-in. This requires no
		// external binaries (no curl/wget) and works on minimal images that have
		// bash. The HTTP path is intentionally ignored — for local dev, verifying
		// the port is open is sufficient.
		test = []string{"CMD-SHELL", fmt.Sprintf("bash -c '(echo >/dev/tcp/localhost/%s) 2>/dev/null'", port)}
	case p.TCPSocket != nil:
		port := p.TCPSocket.Port.String()
		test = []string{"CMD-SHELL", fmt.Sprintf("bash -c '(echo >/dev/tcp/localhost/%s) 2>/dev/null'", port)}
	default:
		return nil
	}

	hc := &comptypes.HealthCheckConfig{
		Test: test,
	}

	if p.PeriodSeconds > 0 {
		d := comptypes.Duration(time.Duration(p.PeriodSeconds) * time.Second)
		hc.Interval = &d
	}
	if p.TimeoutSeconds > 0 {
		d := comptypes.Duration(time.Duration(p.TimeoutSeconds) * time.Second)
		hc.Timeout = &d
	}
	if p.InitialDelaySeconds > 0 {
		d := comptypes.Duration(time.Duration(p.InitialDelaySeconds) * time.Second)
		hc.StartPeriod = &d
	}
	if p.FailureThreshold > 0 {
		r := uint64(p.FailureThreshold)
		hc.Retries = &r
	}

	return hc
}

// translateResources converts K8s ResourceRequirements to a compose Resources struct.
func translateResources(req corev1.ResourceRequirements) comptypes.Resources {
	res := comptypes.Resources{}

	if req.Limits != nil {
		res.Limits = &comptypes.Resource{}
		if cpu, ok := req.Limits[corev1.ResourceCPU]; ok {
			res.Limits.NanoCPUs = comptypes.NanoCPUs(cpu.AsApproximateFloat64())
		}
		if mem, ok := req.Limits[corev1.ResourceMemory]; ok {
			bytes := mem.Value()
			res.Limits.MemoryBytes = comptypes.UnitBytes(bytes)
		}
	}

	if req.Requests != nil {
		res.Reservations = &comptypes.Resource{}
		if cpu, ok := req.Requests[corev1.ResourceCPU]; ok {
			res.Reservations.NanoCPUs = comptypes.NanoCPUs(cpu.AsApproximateFloat64())
		}
		if mem, ok := req.Requests[corev1.ResourceMemory]; ok {
			bytes := mem.Value()
			res.Reservations.MemoryBytes = comptypes.UnitBytes(bytes)
		}
	}

	return res
}

