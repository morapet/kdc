package translator

import (
	comptypes "github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
)

// translatePVC converts a PersistentVolumeClaim into a compose named volume.
// Returns the volume name and a VolumeConfig with a driver label for reference.
func translatePVC(pvc *corev1.PersistentVolumeClaim) (string, comptypes.VolumeConfig) {
	name := pvc.Name
	cfg := comptypes.VolumeConfig{
		Labels: comptypes.Labels{
			"kdc.io/source-kind":      "PersistentVolumeClaim",
			"kdc.io/source-name":      pvc.Name,
			"kdc.io/source-namespace": pvc.Namespace,
		},
	}
	return name, cfg
}
