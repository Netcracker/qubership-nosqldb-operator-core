package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"go.uber.org/zap"
	v1core "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MigratePVCStep handles PVC resize by data migration when direct expansion is not possible
// (storage class does not support expansion, or size decrease is requested).
//
// For each PVC that needs migration it:
//  1. Scales all owning StatefulSets to 0 (full cluster drain, done once for all PVCs)
//  2. Creates a temporary PVC with the desired size
//  3. Runs a migration pod that copies data: old PVC → temp PVC
//  4. Deletes the old PVC
//  5. Creates a new PVC with the original name and desired size
//  6. Runs a migration pod that copies data: temp PVC → new PVC
//  7. Deletes the temp PVC
//  8. Scales all StatefulSets back to their original replica count
//
// The step is idempotent: it checks intermediate PVC state on each reconcile so it can
// resume from partial completion without re-copying already-migrated data.
type MigratePVCStep struct {
	core.DefaultExecutable
	// GetStatefulSetConfigs returns the StatefulSets that mount the migrating PVCs.
	GetStatefulSetConfigs func(ctx core.ExecutionContext) []StatefulSetConfig
	// MigrationImage is the container image used in migration pods (must have /bin/sh and cp).
	MigrationImage     string
	WaitTimeout        int
	PodSecurityContext *v1core.PodSecurityContext
}

func (r *MigratePVCStep) Condition(ctx core.ExecutionContext) (bool, error) {
	pvcs, ok := ctx.Get(constants.MigrationNeededPVCsContextVar).([]*v1core.PersistentVolumeClaim)
	return ok && len(pvcs) > 0, nil
}

func (r *MigratePVCStep) Execute(ctx core.ExecutionContext) error {
	migrationPVCs := ctx.Get(constants.MigrationNeededPVCsContextVar).([]*v1core.PersistentVolumeClaim)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	kubeClient := ctx.Get(constants.ContextClient).(client.Client)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)

	ssetConfigs := r.GetStatefulSetConfigs(ctx)

	log.Info(fmt.Sprintf("PVC migration step: %d PVCs to migrate, %d workloads to drain", len(migrationPVCs), len(ssetConfigs)))

	for _, config := range ssetConfigs {
		if err := scaleWorkload(kubeClient, helperImpl, config, 0, r.WaitTimeout); err != nil {
			return err
		}
	}

	for _, desiredPVC := range migrationPVCs {
		desiredSize := desiredPVC.Spec.Resources.Requests[v1core.ResourceStorage]
		log.Info(fmt.Sprintf("Migrating PVC %s to size %s", desiredPVC.Name, desiredSize.String()))
		if err := r.migratePVC(kubeClient, log, request.Namespace, desiredPVC); err != nil {
			return fmt.Errorf("migrating PVC %s: %w", desiredPVC.Name, err)
		}
	}

	for _, config := range ssetConfigs {
		if err := scaleWorkload(kubeClient, helperImpl, config, int(config.Replicas), r.WaitTimeout); err != nil {
			return err
		}
	}

	return nil
}

func (r *MigratePVCStep) migratePVC(kubeClient client.Client, log *zap.Logger, namespace string, desiredPVC *v1core.PersistentVolumeClaim) error {
	pvcName := desiredPVC.Name
	tempPVCName := pvcName + "-migrating"

	oldPVC := &v1core.PersistentVolumeClaim{}
	oldExists := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: pvcName, Namespace: namespace}, oldPVC) == nil

	tempPVC := &v1core.PersistentVolumeClaim{}
	tempExists := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: tempPVCName, Namespace: namespace}, tempPVC) == nil

	// Phase 1: copy old → temp (skip if old PVC already deleted, meaning phase 1 completed)
	if oldExists {
		if !tempExists {
			log.Info(fmt.Sprintf("Creating temp PVC %s", tempPVCName))
			newTemp := desiredPVC.DeepCopy()
			newTemp.Name = tempPVCName
			newTemp.ResourceVersion = ""
			newTemp.UID = ""
			if err := kubeClient.Create(context.TODO(), newTemp); err != nil {
				return fmt.Errorf("creating temp PVC %s: %w", tempPVCName, err)
			}
			tempPVC = newTemp
			tempExists = true
		}

		podName := "pvc-migrate-" + pvcName + "-to-tmp"
		log.Info(fmt.Sprintf("Running migration pod %s: %s → %s", podName, pvcName, tempPVCName))
		if err := r.runMigrationPod(kubeClient, log, namespace, podName, pvcName, tempPVCName); err != nil {
			return fmt.Errorf("migration pod %s failed: %w", podName, err)
		}

		log.Info(fmt.Sprintf("Deleting old PVC %s", pvcName))
		if err := kubeClient.Delete(context.TODO(), oldPVC); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("deleting old PVC %s: %w", pvcName, err)
		}
	}

	// Phase 2: copy temp → new PVC with original name (skip if already done)
	newPVC := &v1core.PersistentVolumeClaim{}
	newExists := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: pvcName, Namespace: namespace}, newPVC) == nil

	if !newExists {
		if !tempExists {
			return fmt.Errorf("PVC migration in inconsistent state: both old PVC %s and temp PVC %s are gone", pvcName, tempPVCName)
		}

		log.Info(fmt.Sprintf("Creating new PVC %s with desired size", pvcName))
		newPVC = desiredPVC.DeepCopy()
		newPVC.ResourceVersion = ""
		newPVC.UID = ""
		if err := kubeClient.Create(context.TODO(), newPVC); err != nil {
			return fmt.Errorf("creating new PVC %s: %w", pvcName, err)
		}

		podName := "pvc-migrate-tmp-to-" + pvcName
		log.Info(fmt.Sprintf("Running migration pod %s: %s → %s", podName, tempPVCName, pvcName))
		if err := r.runMigrationPod(kubeClient, log, namespace, podName, tempPVCName, pvcName); err != nil {
			return fmt.Errorf("migration pod %s failed: %w", podName, err)
		}
	}

	// Cleanup temp PVC
	if tempExists {
		log.Info(fmt.Sprintf("Deleting temp PVC %s", tempPVCName))
		if err := kubeClient.Delete(context.TODO(), tempPVC); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("deleting temp PVC %s: %w", tempPVCName, err)
		}
	}

	return nil
}

func (r *MigratePVCStep) runMigrationPod(kubeClient client.Client, log *zap.Logger, namespace, podName, srcPVCName, dstPVCName string) error {
	pod := r.migrationPodTemplate(podName, namespace, srcPVCName, dstPVCName)

	if err := kubeClient.Create(context.TODO(), pod); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating migration pod: %w", err)
	}

	log.Info(fmt.Sprintf("Waiting for migration pod %s to complete", podName))

	timeoutDuration := time.Duration(r.WaitTimeout) * time.Second
	if err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeoutDuration, true,
		func(pollCtx context.Context) (bool, error) {
			foundPod := &v1core.Pod{}
			if err := kubeClient.Get(pollCtx, k8stypes.NamespacedName{Name: podName, Namespace: namespace}, foundPod); err != nil {
				return false, err
			}
			switch foundPod.Status.Phase {
			case v1core.PodSucceeded:
				return true, nil
			case v1core.PodFailed:
				// _ = kubeClient.Delete(context.TODO(), foundPod)
				return false, fmt.Errorf("migration pod %s failed: %s", podName, foundPod.Status.Message)
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("Migration pod %s completed, cleaning up", podName))

	foundPod := &v1core.Pod{}
	if err := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: podName, Namespace: namespace}, foundPod); err == nil {
		if err := kubeClient.Delete(context.TODO(), foundPod); err != nil && !k8serrors.IsNotFound(err) {
			log.Warn(fmt.Sprintf("Failed to delete migration pod %s: %v", podName, err))
		}
	}

	return nil
}

func (r *MigratePVCStep) migrationPodTemplate(name, namespace, srcPVCName, dstPVCName string) *v1core.Pod {
	allowPrivilegeEscalation := false

	return &v1core.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": "pvc-migration",
			},
		},
		Spec: v1core.PodSpec{
			SecurityContext: r.PodSecurityContext,
			RestartPolicy:   v1core.RestartPolicyNever,
			Volumes: []v1core.Volume{
				{
					Name: "source",
					VolumeSource: v1core.VolumeSource{
						PersistentVolumeClaim: &v1core.PersistentVolumeClaimVolumeSource{
							ClaimName: srcPVCName,
						},
					},
				},
				{
					Name: "dest",
					VolumeSource: v1core.VolumeSource{
						PersistentVolumeClaim: &v1core.PersistentVolumeClaimVolumeSource{
							ClaimName: dstPVCName,
						},
					},
				},
			},
			Containers: []v1core.Container{
				{
					Name:  "migrate",
					Image: r.MigrationImage,
					SecurityContext: &v1core.SecurityContext{
						Capabilities: &v1core.Capabilities{
							Drop: []v1core.Capability{"ALL"},
						},
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					},
					Command: []string{
						"/bin/sh", "-c",
						"tar -C /source -cf - --exclude=./lost+found . | tar -C /dest -xmf - --no-overwrite-dir && echo 'PVC migration complete'",
					},
					VolumeMounts: []v1core.VolumeMount{
						{Name: "source", MountPath: "/source"},
						{Name: "dest", MountPath: "/dest"},
					},
				},
			},
		},
	}
}
