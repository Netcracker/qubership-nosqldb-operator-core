package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	v1core "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StatefulSetConfig holds the name, namespace and desired replica count for a StatefulSet
// that mounts PVCs being resized.
type StatefulSetConfig struct {
	Name      string
	Namespace string
	Replicas  int32
}

// WaitPVCResizeStep waits for in-progress PVC expansions to complete.
// When the storage driver requires a filesystem resize (FileSystemResizePending condition),
// it performs scale-down/scale-up of the owning StatefulSets with exponential backoff
// until all PVCs reach the desired capacity.
type WaitPVCResizeStep struct {
	core.Executable
	// GetStatefulSetConfigs returns the StatefulSets that mount the resizing PVCs.
	// Called at execution time so it can read dynamic spec values from context.
	GetStatefulSetConfigs func(ctx core.ExecutionContext) []StatefulSetConfig
	WaitTimeout           int
}

func (r *WaitPVCResizeStep) Condition(ctx core.ExecutionContext) (bool, error) {
	resizingPVCs, ok := ctx.Get(constants.ResizingPVCsContextVar).([]*v1core.PersistentVolumeClaim)
	return ok && len(resizingPVCs) > 0, nil
}

func (r *WaitPVCResizeStep) Execute(ctx core.ExecutionContext) error {
	resizingPVCs := ctx.Get(constants.ResizingPVCsContextVar).([]*v1core.PersistentVolumeClaim)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	kubeClient := ctx.Get(constants.ContextClient).(client.Client)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)

	restartRequired := false

	for _, pvc := range resizingPVCs {
		log.Info(fmt.Sprintf("Waiting for PVC %s resize state", pvc.Name))

		desiredSize := pvc.Spec.Resources.Requests[v1core.ResourceStorage]
		pvcRestartRequired, err := helperImpl.WaitForPVCResizeState(pvc.Name, request.Namespace, desiredSize)
		if err != nil {
			return fmt.Errorf("waiting for PVC %s resize state failed: %w", pvc.Name, err)
		}

		if pvcRestartRequired {
			log.Info(fmt.Sprintf("PVC %s requires filesystem resize via pod restart", pvc.Name))
			restartRequired = true
		}
	}

	if !restartRequired {
		return nil
	}

	ssetConfigs := r.GetStatefulSetConfigs(ctx)
	if len(ssetConfigs) == 0 {
		log.Warn("PVC filesystem resize pending but no StatefulSets configured — skipping pod restart")
		return nil
	}

	timeoutDuration := time.Duration(r.WaitTimeout) * time.Second
	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	attempt := 1

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout after %s waiting for PVC filesystem resize to complete", timeoutDuration)
		default:
		}

		delay := 10 * time.Second * time.Duration(1<<uint(attempt-1))
		log.Info(fmt.Sprintf("PVC resize restart attempt %d — scaling down StatefulSets, delay before scale-up: %s", attempt, delay))

		for _, config := range ssetConfigs {
			sts := &appsv1.StatefulSet{}
			if err := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: config.Name, Namespace: config.Namespace}, sts); err != nil {
				return fmt.Errorf("getting StatefulSet %s failed: %w", config.Name, err)
			}
			if err := helperImpl.ScaleStatefulset(sts, 0, r.WaitTimeout); err != nil {
				return fmt.Errorf("scaling StatefulSet %s to 0 failed: %w", config.Name, err)
			}
			if err := helperImpl.WaitForPodsCountByLabel(sts.Spec.Template.Labels, config.Namespace, 0, r.WaitTimeout); err != nil {
				return fmt.Errorf("waiting for StatefulSet %s pods to terminate failed: %w", config.Name, err)
			}
		}

		time.Sleep(delay)

		for _, config := range ssetConfigs {
			sts := &appsv1.StatefulSet{}
			if err := kubeClient.Get(context.TODO(), k8stypes.NamespacedName{Name: config.Name, Namespace: config.Namespace}, sts); err != nil {
				return fmt.Errorf("getting StatefulSet %s failed: %w", config.Name, err)
			}
			if err := helperImpl.ScaleStatefulset(sts, int(config.Replicas), r.WaitTimeout); err != nil {
				return fmt.Errorf("scaling StatefulSet %s to %d failed: %w", config.Name, config.Replicas, err)
			}
		}

		allResized := true
		for _, pvc := range resizingPVCs {
			desiredSize := pvc.Spec.Resources.Requests[v1core.ResourceStorage]
			resized, err := helperImpl.WaitForPVCCapacity(pvc.Name, request.Namespace, desiredSize, 30*time.Second)
			if err != nil {
				return fmt.Errorf("waiting for PVC %s capacity failed: %w", pvc.Name, err)
			}
			if !resized {
				log.Info(fmt.Sprintf("PVC %s not yet at desired capacity %s, will retry", pvc.Name, desiredSize.String()))
				allResized = false
			}
		}

		if allResized {
			log.Info("All PVCs reached desired capacity")
			return nil
		}

		attempt++
	}
}
