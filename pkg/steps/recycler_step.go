package steps

import (
	"fmt"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/utils"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type PVRecyclerStep struct {
	core.DefaultExecutable
	DockerImage        string
	Volumes            []string
	Tolerations        []v12.Toleration
	PVCContextVar      string
	PVNodesContextVar  string
	WaitTimeout        int
	PodSecurityContext *corev1.PodSecurityContext
	Resources          *corev1.ResourceRequirements
	Owner              metav1.Object
	ConditionFunc      func(ctx core.ExecutionContext) (bool, error)
}

func (r *PVRecyclerStep) Execute(ctx core.ExecutionContext) error {
	client := ctx.Get(constants.ContextClient).(client.Client)
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)
	helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	pvcNames := ctx.Get(r.PVCContextVar).([]string)
	nodeLabels := ctx.Get(r.PVNodesContextVar).([]map[string]string)

	pvcSize := len(pvcNames)

	if pvcSize > 0 {
		log.Info("PV Recycling step is started")

		var recyclerPodNames []string
		for key, pvc := range pvcNames {
			nodeSelector := map[string]string{}
			if nodeLabels != nil &&
				len(nodeLabels) > 0 {
				nodeSelector = nodeLabels[key%len(nodeLabels)]
			}

			recyclerPodTemplate := utils.RecyclerPodTemplate(pvc, request.Namespace, r.DockerImage, nodeSelector, r.Tolerations, *r.Resources, r.PodSecurityContext)

			recyclerPodNames = append(recyclerPodNames, recyclerPodTemplate.Name)

			err := helperImpl.CreateRuntimeObject(scheme, r.Owner, recyclerPodTemplate, recyclerPodTemplate.ObjectMeta)
			core.PanicError(err, log.Error, "Recycler pod creation failed")

			log.Debug(fmt.Sprintf("Recycler pod %s is created", recyclerPodTemplate.Name))
		}

		helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)
		err := helperImpl.WaitForPodsCompleted(
			map[string]string{
				constants.Microservice: constants.RecyclerPod,
			},
			request.Namespace,
			pvcSize,
			r.WaitTimeout)

		core.PanicError(err, log.Error, "Recycler Pods Completed status waiting failed")

		log.Debug("Recycler pods are completed")

		recyclerLabels := map[string]string{
			constants.App:          constants.RecyclerPod,
			constants.Microservice: constants.RecyclerPod,
		}

		for _, name := range recyclerPodNames {
			err = core.DeleteRuntimeObject(
				client,
				&v12.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: request.Namespace,
						Labels:    recyclerLabels,
					},
				})

			core.PanicError(err, log.Error, "Recycler Pods deletion failed")
		}

		err = helperImpl.WaitForPodsCountByLabel(
			recyclerLabels,
			request.Namespace,
			0,
			r.WaitTimeout)

		core.PanicError(err, log.Error, "Recycler Pods Terminated status waiting failed")

		log.Debug("Recycler pods are flushed")
	} else {
		log.Debug("PV Recycling step is skipped due to a Nodes or PVC list are not found in the execution context.")
	}

	return nil
}

func (r *PVRecyclerStep) Condition(ctx core.ExecutionContext) (bool, error) {
	if r.ConditionFunc != nil {
		return r.ConditionFunc(ctx)
	}
	return core.GetCurrentDeployType(ctx) == core.CleanDeploy, nil
}
