package steps

import (
	"fmt"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// WaitForPVCExpansionStep waits for PVCs to finish expanding after CreatePVCStep submits a resize.
// When the storage class requires a pod restart to complete filesystem resize (FileSystemResizePending),
// OnNeedsRestart is called so the caller can bounce whichever workload owns the PVCs.
type WaitForPVCExpansionStep struct {
	core.DefaultExecutable
	WaitTimeout    int
	PVCNamesVar    string
	StorageSizes   []string
	OnNeedsRestart func(ctx core.ExecutionContext) error
}

func (r *WaitForPVCExpansionStep) Execute(ctx core.ExecutionContext) error {
	resizeNeeded, _ := ctx.Get(constants.PVCResizeNeeded).(bool)
	if !resizeNeeded {
		return nil
	}

	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)

	pvcNames, _ := ctx.Get(r.PVCNamesVar).([]string)

	anyNeedsRestart := false
	for i, pvcName := range pvcNames {
		desiredSize, err := r.desiredSizeForIndex(i)
		if err != nil {
			return fmt.Errorf("determining desired size for PVC %s: %w", pvcName, err)
		}

		needsRestart, err := helperImpl.WaitForPVCExpansion(pvcName, request.Namespace, desiredSize, r.WaitTimeout)
		if err != nil {
			return fmt.Errorf("waiting for PVC %s expansion: %w", pvcName, err)
		}
		if needsRestart {
			anyNeedsRestart = true
		}
	}

	if anyNeedsRestart && r.OnNeedsRestart != nil {
		return r.OnNeedsRestart(ctx)
	}

	return nil
}

func (r *WaitForPVCExpansionStep) Condition(ctx core.ExecutionContext) (bool, error) {
	return true, nil
}

func (r *WaitForPVCExpansionStep) desiredSizeForIndex(idx int) (resource.Quantity, error) {
	if len(r.StorageSizes) == 0 {
		return resource.Quantity{}, fmt.Errorf("storage sizes not configured")
	}
	sizeStr := r.StorageSizes[idx%len(r.StorageSizes)]
	qty, err := resource.ParseQuantity(sizeStr)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid storage size %q: %w", sizeStr, err)
	}
	return qty, nil
}
