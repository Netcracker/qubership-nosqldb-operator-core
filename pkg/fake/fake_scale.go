package fake

import (
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"

	"go.uber.org/zap"
	v12 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type FakeScaleDeployment struct {
	core.DefaultExecutable
}

func (r *FakeScaleDeployment) Execute(ctx core.ExecutionContext) error {
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	fakeName := "Fake"
	dcList := &v12.DeploymentList{}
	err := helperImpl.ListRuntimeObjectsByLabels(dcList, request.Namespace, map[string]string{"name": fakeName})
	core.PanicError(err, log.Error, "Error happened during deployment listing")

	return helperImpl.ScaleDeployment(&dcList.Items[0], 2, 1)
}

func (r *FakeScaleDeployment) Condition(ctx core.ExecutionContext) (bool, error) {
	return true, nil
}
