package steps

import (
	"fmt"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/utils"
	"go.uber.org/zap"
	v1core "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CreatePVCStep struct {
	core.Executable
	Storage           *types.StorageRequirements
	NameFormat        string
	LabelSelector     map[string]string
	ContextVarToStore string
	WaitTimeout       int
	PVCCount          func(ctx core.ExecutionContext) int
	StartIndex        int
	Owner             v1.Object
	WaitPVCBound      bool
	AccessMode        v1core.PersistentVolumeAccessMode
}

func (r *CreatePVCStep) Validate(ctx core.ExecutionContext) error {
	storage := r.Storage

	if storage == nil ||
		len((*storage).Size) == 0 ||
		((*storage).MatchLabelSelectors == nil &&
			(*storage).Volumes == nil &&
			(*storage).StorageClasses == nil) {
		return &core.ExecutionError{Msg: "Storage size should be set with volumes or storage classes or label selectors"}
	}

	return nil
}

func (r *CreatePVCStep) Execute(ctx core.ExecutionContext) error {
	var request reconcile.Request = ctx.Get(constants.ContextRequest).(reconcile.Request)
	scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	log.Info("PVC Creation/Checking step is started")
	maxSize := r.PVCCount(ctx)
	log.Debug(fmt.Sprintf("PVC count is: %v", maxSize))

	if r.AccessMode == "" {
		r.AccessMode = v1core.ReadWriteOnce
	}

	existingPVCList := &v1core.PersistentVolumeClaimList{}
	listErr := helperImpl.ListRuntimeObjectsByLabels(existingPVCList, request.Namespace, r.LabelSelector)
	core.PanicError(listErr, log.Error, "Listing existing PVCs failed")
	existingPVCs := make(map[string]v1core.PersistentVolumeClaim, len(existingPVCList.Items))
	for _, pvc := range existingPVCList.Items {
		existingPVCs[pvc.Name] = pvc
	}

	var pvcArray []string
	var resizingPVCs []*v1core.PersistentVolumeClaim

	for i := r.StartIndex; i < (maxSize + r.StartIndex); i++ {
		template := utils.PVCTemplate(*r.Storage, i, r.NameFormat, r.LabelSelector, request.Namespace, r.AccessMode)

		if foundPvc, exists := existingPVCs[template.Name]; exists {
			log.Debug(fmt.Sprintf("PVC %s already exists, checking size", template.Name))

			desiredSize := template.Spec.Resources.Requests[v1core.ResourceStorage]
			currentSize := foundPvc.Spec.Resources.Requests[v1core.ResourceStorage]

			switch desiredSize.Cmp(currentSize) {
			case 1:
				log.Info(fmt.Sprintf("PVC %s resize requested: %s -> %s", template.Name, currentSize.String(), desiredSize.String()))
				resizeInProgress, err := helperImpl.ResizePVC(template.Name, request.Namespace, desiredSize)
				core.PanicError(err, log.Error, "Resizing PVC "+template.Name+" failed")
				if resizeInProgress {
					resizingPVCs = append(resizingPVCs, template.DeepCopy())
				}
			case -1:
				log.Info(fmt.Sprintf("PVC %s decrease from %s to %s requires migration (not handled in this step)", template.Name, currentSize.String(), desiredSize.String()))
			}

			if len(r.Storage.Annotations) > 0 {
				err := helperImpl.PatchPVCAnnotations(template.Name, request.Namespace, r.Storage.Annotations)
				core.PanicError(err, log.Error, "Patching annotations on PVC "+template.Name+" failed")
			}
		} else {
			err := helperImpl.CreateRuntimeObject(scheme, r.Owner, template, template.ObjectMeta)
			core.PanicError(err, log.Error, "Creating of PVC "+template.ObjectMeta.Name+" failed")
		}

		pvcArray = append(pvcArray, template.ObjectMeta.Name)
	}

	ctx.Set(constants.ResizingPVCsContextVar, resizingPVCs)

	if r.WaitPVCBound {
		for _, pvcName := range pvcArray {
			err := helperImpl.WaitForPVCBound(pvcName, request.Namespace, r.WaitTimeout)
			core.PanicError(err, log.Error, "PVC "+pvcName+" 'Bound' status waiting failed")
			log.Debug(fmt.Sprintf("PVC %s is bound", pvcName))
		}
	}

	if fromCtx, ok := ctx.Get(r.ContextVarToStore).([]string); ok {
		pvcArray = append(fromCtx, pvcArray...)
	}
	ctx.Set(r.ContextVarToStore, pvcArray)
	return nil
}

func (r *CreatePVCStep) Condition(ctx core.ExecutionContext) (bool, error) {
	return true, nil
}
