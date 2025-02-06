package steps

import (
	"context"
	"fmt"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/types"
	"go.uber.org/zap"
	v1core "k8s.io/api/core/v1"
	kTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type StoreNodesStep struct {
	core.Executable
	Storage           *types.StorageRequirements
	ContextVarToStore string
}

func (r *StoreNodesStep) Validate(ctx core.ExecutionContext) error {
	return nil
}

func (r *StoreNodesStep) Execute(ctx core.ExecutionContext) error {

	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	log.Info("Store Nodes step is started")
	// Are nodes set by request?
	storage := &r.Storage
	nodes := r.Storage.NodeLabels
	if len(nodes) < 1 {

		//log.Warn("Nodes are not specified. Trying to get it by volumes may cause RBAC errors...")

		client := ctx.Get(constants.ContextClient).(client.Client)
		request := ctx.Get(constants.ContextRequest).(reconcile.Request)

		volumeNames := []string{}

		//Try to get nodes by PVs list (restricted error)
		if storage != nil &&
			(*storage).Volumes != nil &&
			len((*storage).Volumes) > 0 {
			volumeNames = (*storage).Volumes
		} else {
			//log.Warn("Nodes are not specified. Trying to get it by volumes may cause RBAC errors...")
		}
		//} else {
		//	// TODO: Except auto-provisioning case. Still not checked
		//	// Trying to get PVs by already processed PVCs
		//	pvcNames := ctx.Get(utils.PvcNames).([]string)
		//
		//	for _, pvcName := range pvcNames {
		//
		//		foundPvc := &v1core.PersistentVolumeClaim{}
		//		err := client.Get(context.TODO(), types.NamespacedName{
		//			Name: pvcName, Namespace: request.Namespace,
		//		}, foundPvc)
		//
		//		if err != nil {
		//			return &core.ExecutionError{Msg: "Error on trying to get Volume name from \"" + pvcName + "\"'s name. Error: " + err.Error()}
		//		}
		//
		//		volumeNames = append(volumeNames, foundPvc.Spec.VolumeName)
		//	}
		//}

		for _, pvName := range volumeNames {
			log.Debug("Trying to get node from " + pvName + " PV")
			pv := &v1core.PersistentVolume{}

			err := client.Get(context.Background(), kTypes.NamespacedName{
				Name: pvName, Namespace: request.Namespace,
			}, pv)

			//Try to get pv. If error - restricted env
			if err != nil {
				//return err
				log.Error("Error on trying to get pv " + pvName + "'s node. Restricted environment? Error: " + err.Error())
				break
				//return &core.ExecutionError{Msg: "Error on trying to get pv \"" + pvName + "\"'s node. Restricted environment? Error: " + err.Error()}
			}

			if pv.Spec.HostPath != nil {
				//else, get pv' node
				nodes = append(nodes, map[string]string{constants.KubeHostName: pv.ObjectMeta.Labels["node"]})
			}
		}

		if len(nodes) != len(volumeNames) {
			log.Error(fmt.Sprintf("Nodes list ( %s ) from volumes ( %s )", nodes, volumeNames))
			core.PanicError(&core.ExecutionError{Msg: "Got unequal Nodes count from Volumes"}, log.Error, "Nodes step failed")
		}

	}

	if len(nodes) > 0 {
		log.Debug(fmt.Sprintf("Stored nodes list ( %s ) to context", nodes))
		ctx.Set(r.ContextVarToStore, nodes)
	} else {
		log.Debug("Nodes list is empty")
		ctx.Set(r.ContextVarToStore, []map[string]string{})
	}

	return nil
}

func (r *StoreNodesStep) Condition(ctx core.ExecutionContext) (bool, error) {
	return true, nil
}
