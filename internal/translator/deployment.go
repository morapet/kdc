package translator

import (
	"fmt"
	"time"

	comptypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/morapet/kdc/internal/filter"
	kdctypes "github.com/morapet/kdc/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// translateDeployment converts a Deployment into compose services.
// Returns (translated services, injected replacement services, info messages).
func translateDeployment(
	d *appsv1.Deployment,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string) {
	ns := d.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return translatePodSpec(d.Name, ns, d.Spec.Template.Spec, cmIndex, secIndex, defaultNamespace, d.Labels, eng)
}

// translatePod converts a standalone Pod into compose services.
func translatePod(
	p *corev1.Pod,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string) {
	ns := p.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return translatePodSpec(p.Name, ns, p.Spec, cmIndex, secIndex, defaultNamespace, p.Labels, eng)
}

func translatePodSpec(
	ownerName string,
	namespace string,
	spec corev1.PodSpec,
	cmIndex map[string]map[string]string,
	secIndex map[string]map[string]string,
	defaultNamespace string,
	sourceLabels map[string]string,
	eng *filter.Engine,
) (services, replacements []comptypes.ServiceConfig, messages []string) {
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

		svc := translateContainer(name, namespace, c, spec, volSources, cmIndex, secIndex, defaultNamespace)
		svc.Labels = comptypes.Labels{
			kdctypes.AnnotationSourceKind:      "Deployment",
			kdctypes.AnnotationSourceName:      ownerName,
			kdctypes.AnnotationSourceNamespace: namespace,
		}
		services = append(services, svc)
	}
	return services, replacements, messages
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
) comptypes.ServiceConfig {
	svc := comptypes.ServiceConfig{
		Name:        serviceName,
		Image:       c.Image,
		Environment: comptypes.MappingWithEquals{},
	}

	// Command / args
	if len(c.Command) > 0 {
		svc.Entrypoint = comptypes.ShellCommand(c.Command)
	}
	if len(c.Args) > 0 {
		svc.Command = comptypes.ShellCommand(c.Args)
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
	svc.Volumes = translateVolumeMounts(c.VolumeMounts, volSources)

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

	return svc
}

// translateVolumeMounts converts container volume mounts to compose volume entries.
// ConfigMap and Secret mounts become compose configs/secrets references.
// PVC mounts become named volume references.
// EmptyDir mounts become anonymous volumes.
func translateVolumeMounts(mounts []corev1.VolumeMount, volSources map[string]volumeSource) []comptypes.ServiceVolumeConfig {
	var vols []comptypes.ServiceVolumeConfig
	for _, m := range mounts {
		src, ok := volSources[m.Name]
		if !ok {
			continue
		}
		switch src.kind {
		case "configMap":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:   "bind",
				Source: fmt.Sprintf("./.kdc/configs/%s", src.name),
				Target: m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "secret":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:   "bind",
				Source: fmt.Sprintf("./.kdc/secrets/%s", src.name),
				Target: m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "pvc":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:   "volume",
				Source: src.name,
				Target: m.MountPath,
				ReadOnly: m.ReadOnly,
			})
		case "emptyDir":
			vols = append(vols, comptypes.ServiceVolumeConfig{
				Type:   "tmpfs",
				Target: m.MountPath,
			})
		}
	}
	return vols
}

// translateProbe converts a K8s Probe to a compose HealthCheckConfig.
func translateProbe(p *corev1.Probe) *comptypes.HealthCheckConfig {
	var test []string

	switch {
	case p.Exec != nil:
		test = append([]string{"CMD-SHELL"}, p.Exec.Command...)
	case p.HTTPGet != nil:
		port := p.HTTPGet.Port.String()
		path := p.HTTPGet.Path
		if path == "" {
			path = "/"
		}
		test = []string{"CMD-SHELL", fmt.Sprintf("curl -sf http://localhost:%s%s", port, path)}
	case p.TCPSocket != nil:
		port := p.TCPSocket.Port.String()
		test = []string{"CMD-SHELL", fmt.Sprintf("nc -z localhost %s", port)}
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

