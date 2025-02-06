package fake

import (
	"fmt"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/steps"
	"go.uber.org/zap"
	v12 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Fake struct {
	core.MicroServiceCompound
}

type FakeBuilder struct {
	core.ExecutableBuilder
}

func (r *FakeBuilder) Build(ctx core.ExecutionContext) core.Executable {
	spec := ctx.Get(constants.ContextSpec).(*FakeService)
	pvcSelector := map[string]string{
		"name": "Fake",
	}

	fake := Fake{}
	fake.ServiceName = "Fake"
	fake.CalcDeployType = func(ctx core.ExecutionContext) (core.MicroServiceDeployType, error) {
		request := ctx.Get(constants.ContextRequest).(reconcile.Request)
		log := ctx.Get(constants.ContextLogger).(*zap.Logger)
		helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)

		pvcList := &v12.PersistentVolumeClaimList{}
		err := helperImpl.ListRuntimeObjectsByLabels(pvcList, request.Namespace, pvcSelector)
		var result core.MicroServiceDeployType
		if err != nil {
			result = core.Empty
		} else if len(pvcList.Items) == 0 {
			result = core.CleanDeploy
		} else {
			result = core.Update
		}

		if err == nil {
			log.Debug(fmt.Sprintf("%s deploy mode is used for %s service", result, "Fake"))
		}

		return result, err
	}

	pvcContext := fmt.Sprintf("pvcNames%v", 0)
	nodesContext := fmt.Sprintf("pvNodeNames%v", 0)
	storage := spec.Spec.Storage
	fake.AddStep(&steps.CreatePVCStep{
		Storage:           storage,
		NameFormat:        "fake-data-%v",
		LabelSelector:     pvcSelector,
		ContextVarToStore: pvcContext,
		PVCCount: func(ctx core.ExecutionContext) int {
			return 1
		},
		WaitTimeout:  10,
		Owner:        spec,
		WaitPVCBound: false,
	})
	fake.AddStep(&steps.StoreNodesStep{
		Storage:           storage,
		ContextVarToStore: nodesContext,
	})

	if spec.Spec.VaultRegistration.Enabled {
		fake.AddStep(&steps.MoveSecretToVault{
			SecretName:            "fakeSecretName",
			PolicyName:            "fakePolicyName",
			Policy:                "fakePolicy",
			VaultRegistration:     &spec.Spec.VaultRegistration,
			CtxVarToStorePassword: "password",
			ConditionFunc:         nil,
		})
	}
	fake.AddStep(&FakeDeployment{})
	fake.AddStep(&FakeScaleDeployment{})

	return &fake
}
