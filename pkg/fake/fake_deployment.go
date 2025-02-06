package fake

import (
	"fmt"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/utils"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type FakeDeployment struct {
	core.DefaultExecutable
}

func (r *FakeDeployment) Execute(ctx core.ExecutionContext) error {
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)
	spec := ctx.Get(constants.ContextSpec).(*FakeService)

	log.Info("Fake Deployment initialization step started")

	pvcNames := ctx.Get(fmt.Sprintf(fmt.Sprintf("pvcNames%v", 0))).([]string)
	nodeLabels := ctx.Get(fmt.Sprintf("pvNodeNames%v", 0)).([]map[string]string)

	nodeSelector := map[string]string{}
	if nodeLabels != nil &&
		len(nodeLabels) > 0 {
		nodeSelector = nodeLabels[0]
	}
	dc := FakeDeploymentTemplate(
		pvcNames[0],
		request.Namespace,
		"docker.io:fake_latest",
		nodeSelector,
		spec.Spec.Policies.Tolerations,
		*spec.Spec.Resources,
		spec.Spec.PodSecurityContext)

	utils.VaultPodSpec(&dc.Spec.Template.Spec, []string{"/fake.sh"}, spec.Spec.VaultRegistration)

	err := helperImpl.DeleteDeploymentAndPods(dc.Name, request.Namespace, 10)

	if err != nil {
		return err
	}

	owner := &(*spec)
	err = helperImpl.CreateRuntimeObject(scheme, owner, dc, dc.ObjectMeta)
	core.PanicError(err, log.Error, "Error happened on processing fake deployment config")

	core.PanicError(err, log.Error, "Pods waiting failed")

	return nil
}

func (r *FakeDeployment) Condition(ctx core.ExecutionContext) (bool, error) {
	return true, nil
}
