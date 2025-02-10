package utils

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func PVCTemplate(storage types.StorageRequirements, pvcId int, nameFormat string, labels map[string]string, namespace string, accessMode v1.PersistentVolumeAccessMode) *v1.PersistentVolumeClaim {
	name := nameFormat
	if strings.Contains(nameFormat, "%v") {
		name = fmt.Sprintf(nameFormat, pvcId)
	}

	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: make(map[string]string),
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{accessMode},
		},
	}

	if len(storage.Size) > 0 {
		properIndex := pvcId % len(storage.Size)
		volumeSize := storage.Size[properIndex]
		pvc.Spec.Resources = v1.VolumeResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): resource.MustParse(volumeSize),
			},
		}
	}

	if len(storage.StorageClasses) > 0 {
		properIndex := pvcId % len(storage.StorageClasses)
		class := &storage.StorageClasses[properIndex]
		pvc.ObjectMeta.Annotations["volume.beta.kubernetes.io/storage-class"] = *class
		pvc.Spec.StorageClassName = class
	}

	if len(storage.Volumes) > 0 {
		properIndex := pvcId % len(storage.Volumes)
		pv := storage.Volumes[properIndex]
		pvc.Spec.VolumeName = pv
	}

	if len(storage.MatchLabelSelectors) > 0 {
		properIndex := pvcId % len(storage.MatchLabelSelectors)
		labelsMap := storage.MatchLabelSelectors[properIndex]
		pvc.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: labelsMap,
		}
	}

	return pvc
}

func SecretTemplate(name string, values map[string]string, namespace string) *v1.Secret {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: make(map[string]string),
		},
	}
	data := map[string][]byte{}
	for key, value := range values {
		data[key] = []byte(value)
	}

	secret.Data = data

	return secret
}

func ServiceAccountSecretTemplate(name string, saName string, namespace string) *v1.Secret {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": saName,
			},
		},
		Type: v1.SecretTypeServiceAccountToken,
	}

	return secret
}

func RecyclerPodTemplate(pvcName string, namespace string, image string, nodeSelector map[string]string,
	tolerations []v1.Toleration, res v1.ResourceRequirements, securityContext *v1.PodSecurityContext) *v1.Pod {
	podName := fmt.Sprintf(constants.RecyclerNameTemplate, pvcName)
	allowPrivilegeEscalation := false

	affinity := v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      constants.Microservice,
								Operator: "In",
								Values: []string{
									constants.RecyclerPod,
								},
							},
						},
					},
					TopologyKey: constants.KubeHostName,
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				constants.App:          constants.RecyclerPod,
				constants.Microservice: constants.RecyclerPod,
			},
		},
		Spec: v1.PodSpec{
			SecurityContext: securityContext,
			RestartPolicy:   v1.RestartPolicyNever,
			Volumes: []v1.Volume{
				v1.Volume{
					Name: podName,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
			Containers: []v1.Container{
				v1.Container{
					Name:  fmt.Sprintf(constants.RecyclerNameTemplate, "container"),
					Image: image,
					SecurityContext: &v1.SecurityContext{
						Capabilities: &v1.Capabilities{
							Drop: []v1.Capability{"ALL"},
						},
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					},
					Command: []string{
						"/bin/sh",
						"-c",
						"set -x && echo \"clearing pvc\" && ls -lah /scrub && rm -rf /scrub/* && rm -rf /scrub/.ssh && test -z \"$(ls -A /scrub)\" && ls -lah /scrub || exit 1",
					},
					VolumeMounts: []v1.VolumeMount{
						v1.VolumeMount{
							Name:      podName,
							MountPath: "/scrub",
						},
					},
					Resources: res,
				},
			},
			NodeSelector: nodeSelector,
			Affinity:     &affinity,
			Tolerations:  tolerations,
		},
	}

	return pod
}

func SimpleServiceTemplate(name string, labels map[string]string, selectors map[string]string, ports map[string]int32, namespace string) *v1.Service {

	sp := []v1.ServicePort{}

	for k, v := range ports {
		sp = append(sp, v1.ServicePort{
			Name:       k,
			Port:       v,
			TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: v},
		})
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports:    sp,
			Selector: selectors,
		},
	}

}

func HeadlessServiceTemplate(name string, labels map[string]string, selectors map[string]string, ports map[string]int32, namespace string) *v1.Service {
	service := SimpleServiceTemplate(name, labels, selectors, ports, namespace)

	service.Spec.Type = v1.ServiceTypeClusterIP
	service.Spec.ClusterIP = "None"

	return service

}

func MultiportServiceTemplate(name string, labels, selectors map[string]string, ports *[]v1.ServicePort, namespace string) *v1.Service {

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports:    *ports,
			Selector: selectors,
		},
	}

}

func GetEnvTemplateForVault(envName string, secretName string, secretKey string, vaultPath string) v1.EnvVar {
	return v1.EnvVar{
		Name:  envName,
		Value: fmt.Sprintf("vault:%s/%s#%s", vaultPath, secretName, secretKey),
	}
}

var VaultMounthPath = "/vault"
var VaultEnvName = "vault-env"

func GetVaultEnvPath() string {
	return fmt.Sprintf("%s/%s", VaultMounthPath, VaultEnvName)
}

func GetInitContainerTemplateForVault(dockerImage string, resources *v1.ResourceRequirements) []v1.Container {
	allowPrivilegeEscalation := false
	initContainer := []v1.Container{
		{
			Name:  "copy-vault-env",
			Image: dockerImage,
			SecurityContext: &v1.SecurityContext{
				Capabilities: &v1.Capabilities{
					Drop: []v1.Capability{"ALL"},
				},
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			},
			VolumeMounts: []v1.VolumeMount{
				{
					MountPath: VaultMounthPath,
					Name:      VaultEnvName,
				},
			},
			Command: []string{
				"sh",
				"-c",
				fmt.Sprintf("cp /usr/local/bin/vault-env %s/", VaultMounthPath),
			},
			Resources: *resources,
		},
	}
	return initContainer
}

func GetVaultVolume() v1.Volume {
	volume := v1.Volume{
		Name: VaultEnvName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium: "Memory",
			},
		},
	}
	return volume
}

func GetVaultVolumeMount() v1.VolumeMount {
	volumeMount := v1.VolumeMount{
		MountPath: VaultMounthPath,
		Name:      VaultEnvName,
	}
	return volumeMount
}

func GetSecretEnvVar(envName string, secretName string, secretKey string) v1.EnvVar {
	return v1.EnvVar{
		Name: envName,
		ValueFrom: &v1.EnvVarSource{
			SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: secretName,
				},
				Key: secretKey,
			},
		},
	}
}

func GetPlainTextEnvVar(envName string, value string) v1.EnvVar {
	return v1.EnvVar{
		Name:  envName,
		Value: value,
	}
}

func GetVaultRegistrationEnv(url string, role string, authMethod string) []v1.EnvVar {
	envValue := []v1.EnvVar{
		{
			Name:  "VAULT_SKIP_VERIFY",
			Value: "True",
		},
		{
			Name:  "VAULT_ADDR",
			Value: url,
		},
		{
			Name:  "VAULT_PATH",
			Value: authMethod,
		},
		{
			Name:  "VAULT_ROLE",
			Value: role,
		},
		{
			Name:  "VAULT_IGNORE_MISSING_SECRETS",
			Value: "False",
		},
	}
	return envValue
}

func GetProxyService(name string, namespace string, labels map[string]string, externalName string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Type:         v1.ServiceTypeExternalName,
			ExternalName: externalName,
		},
	}
}

func VaultPodSpec(podSpec *v1.PodSpec, entrypoint []string, vault types.VaultRegistration) {
	if vault.Enabled {
		vaultCommand := []string{
			GetVaultEnvPath(),
		}

		vaultVolumes := []v1.Volume{GetVaultVolume()}
		for _, el := range podSpec.Volumes {
			matched := false
			for _, srcEl := range vaultVolumes {
				matched = el.Name == srcEl.Name
				if matched {
					break
				}
			}

			if !matched {
				vaultVolumes = append(vaultVolumes, el)
			}
		}
		podSpec.Volumes = vaultVolumes

		podSpec.InitContainers = GetInitContainerTemplateForVault(vault.DockerImage, vault.InitContainerResources)

		vaultMounts := []v1.VolumeMount{GetVaultVolumeMount()}
		for _, el := range podSpec.Containers[0].VolumeMounts {
			matched := false
			for _, srcEl := range vaultMounts {
				matched = el.Name == srcEl.Name
				if matched {
					break
				}
			}

			if !matched {
				vaultMounts = append(vaultMounts, el)
			}
		}
		podSpec.Containers[0].VolumeMounts = vaultMounts

		podSpec.Containers[0].Command = vaultCommand
		if entrypoint != nil {
			podSpec.Containers[0].Args = entrypoint
		}

		vaultEnvs := GetVaultRegistrationEnv(vault.Url, vault.Role, vault.Method)
		for _, el := range podSpec.Containers[0].Env {
			matched := false
			for _, srcEl := range vaultEnvs {
				matched = el.Name == srcEl.Name
				if matched {
					break
				}
			}

			if !matched {
				vaultEnvs = append(vaultEnvs, el)
			}
		}
		podSpec.Containers[0].Env = vaultEnvs
	}
}

func TLSSpecUpdate(depl *v1.PodSpec, rootCertPath string, tls types.TLS) {
	RootCert := "cert-volume"
	if !tls.Enabled {
		return
	}
	volProj := []v1.VolumeProjection{
		{
			Secret: &v1.SecretProjection{
				LocalObjectReference: v1.LocalObjectReference{
					Name: tls.CertificateSecretName,
				},
				Items: []v1.KeyToPath{
					{
						Path: tls.RootCAFileName,
						Key:  tls.RootCAFileName,
					},
				},
			},
		}}

	volume := []v1.Volume{
		{
			Name: RootCert,
			VolumeSource: v1.VolumeSource{
				Projected: &v1.ProjectedVolumeSource{
					Sources:     volProj,
					DefaultMode: nil,
				},
			},
		},
	}

	volumeMount := []v1.VolumeMount{{
		Name:      RootCert,
		MountPath: rootCertPath,
	}}

	depl.Volumes = append(depl.Volumes, volume...)
	depl.Containers[0].VolumeMounts = append(depl.Containers[0].VolumeMounts, volumeMount...)

	depl.Containers[0].Env = append(depl.Containers[0].Env,
		GetPlainTextEnvVar("TLS_ENABLED", strconv.FormatBool(tls.Enabled)),
		GetPlainTextEnvVar("TLS_ROOTCERT", rootCertPath+tls.RootCAFileName))
}

func TLSServerSpecUpdate(depl *v1.PodSpec, tls types.TLS, secretName string, mountPath string) {
	if !tls.Enabled {
		return
	}

	depl.Volumes = append(depl.Volumes,
		v1.Volume{
			Name: secretName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		},
	)

	depl.Containers[0].VolumeMounts = append(depl.Containers[0].VolumeMounts,
		v1.VolumeMount{
			Name:      secretName,
			ReadOnly:  true,
			MountPath: mountPath,
		},
	)

	depl.Containers[0].Env = append(depl.Containers[0].Env,
		GetPlainTextEnvVar("INTERNAL_TLS_ENABLED", strconv.FormatBool(tls.Enabled)),
		GetPlainTextEnvVar("INTERNAL_TLS_ROOTCERT", mountPath+tls.RootCAFileName),
		GetPlainTextEnvVar("INTERNAL_TLS_CERTIFICATE_FILENAME", mountPath+tls.SignedCRTFileName),
		GetPlainTextEnvVar("INTERNAL_TLS_KEY_FILENAME", mountPath+tls.PrivateKeyFileName),
		GetPlainTextEnvVar("INTERNAL_TLS_PATH", mountPath),
	)
}

func IsTLSEnableForDBAAS(aggregatorRegistrationAddress string, tlsEnabled bool) bool {
	return strings.Contains(aggregatorRegistrationAddress, "https") && tlsEnabled
}

func GetHTTPPort(tlsEnabled bool) int32 {
	var port int32 = 8080
	if tlsEnabled {
		port = 8443
	}
	return port
}

func GetHTTPProtocol(tlsEnabled bool) string {
	if tlsEnabled {
		return "https"
	}
	return "http"
}
