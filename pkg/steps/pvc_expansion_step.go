package steps

import (
	"fmt"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// WaitForPVCExpansionStep waits for PVCs to finish expanding after CreatePVCStep submits a resize.
// When the storage class requires a pod restart to complete filesystem resize (FileSystemResizePending),
// OnNeedsRestart is called so the caller can bounce whichever workload owns the PVCs.
type WaitForPVCExpansionStep struct {
	core.DefaultExecutable
	WaitTimeout    int
	PVCNamesVar    string
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
	for _, pvcName := range pvcNames {
		needsRestart, err := helperImpl.WaitForPVCExpansion(pvcName, request.Namespace, r.WaitTimeout)
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
