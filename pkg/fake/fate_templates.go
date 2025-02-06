package fake

import (
	v12 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FakeDeploymentTemplate(pvcName string, namespace string, image string, nodeSelector map[string]string,
	tolerations []v1.Toleration, resources v1.ResourceRequirements, securityContext *v1.PodSecurityContext) *v12.Deployment {
	var replicas int32 = 1
	storage := "fake-storage"
	fakeName := "Fake"

	dc := &v12.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fakeName,
			Namespace: namespace,
			Labels: map[string]string{
				"name": fakeName,
			},
		},
		Spec: v12.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": fakeName,
				},
			},
			Replicas: &replicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Labels: map[string]string{
						"name": fakeName,
					},
				},
				Spec: v1.PodSpec{
					SecurityContext: securityContext,
					Containers: []v1.Container{
						v1.Container{
							Name:  fakeName,
							Image: image,
							Ports: []v1.ContainerPort{
								v1.ContainerPort{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      "TCP",
								},
							},
							Resources: resources,
							VolumeMounts: []v1.VolumeMount{
								v1.VolumeMount{
									Name:      storage,
									MountPath: "/" + storage,
								},
							},
						},
					},
					Tolerations:  tolerations,
					NodeSelector: nodeSelector,
					Volumes: []v1.Volume{
						v1.Volume{
							Name: storage,
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}

	return dc
}
