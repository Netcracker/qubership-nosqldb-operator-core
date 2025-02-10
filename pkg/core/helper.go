package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"go.uber.org/zap"
	v14 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type KubernetesHelper interface {
	execCommand(name string, arg []string, stdInData string) ([]byte, error)
	OpensslCommand(arg []string) ([]byte, error)
	WaitForPVCBound(pvcName string, namespace string, waitSeconds int) error
	WaitForPodsReady(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error
	WaitForPodsCompleted(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error
	WaitForPodsCountByLabel(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error
	WaitForDeploymentReady(deployName string, namespace string, waitSeconds int) error
	WaitForTestsReady(deployName string, namespace string, waitSeconds int) error
	ExecRemote(log *zap.Logger, kubeConfig *rest.Config, podName string, namespace string, containerName string, command string, args []string) (string, error)
	GetPodLogs(kubeConfig *rest.Config, podName string, namespace string, containerName string, tailLines *int64, previous bool) (string, error)
	CreateRuntimeObject(scheme *runtime.Scheme, owner v12.Object, object client.Object, meta v12.ObjectMeta) error //todo collapse object and meta params
	ListPods(namespace string, labelSelectors map[string]string) (*v1.PodList, error)
	GetForceKey() bool
	GetOwnerKey() bool
	ListRuntimeObjectsByLabels(object client.ObjectList, namespace string, labelSelectors map[string]string) error
	DeleteDeploymentAndPods(dcName string, namespace string, waitSeconds int) error
	DeleteStatefulsetAndPods(ssName string, namespace string, waitSeconds int) error
	DeleteRCAndPods(ssName string, namespace string, waitSeconds int) error
	GetDeploymentTypeByPVC(ctx ExecutionContext, serviceName string, pvcSelector map[string]string) (MicroServiceDeployType, error)
	ScaleDeployment(obj *v14.Deployment, replicas, timeout int) error
	ScaleDeploymentByLabels(labels map[string]string, namespace string, replicas, timeout int) error
	UpdateDeploymentByLabels(labels map[string]string, namespace string, updateFunc func(depl *v14.Deployment)) error
	ScaleStatefulset(obj *v14.StatefulSet, replicas, timeout int) error
	ScaleReplicationController(obj *v1.ReplicationController, replicas, timeout int) error
	RestartPod(pod *v1.Pod, namespace string, waitSeconds int) error
	GetConfigMap(name, namespace string) (*v1.ConfigMap, error)
	//CheckSpecChange(ctx ExecutionContext, spec interface{}, serviceName string) (bool, error)
}

type DefaultKubernetesHelperImpl struct {
	KubernetesHelper
	ForceKey bool
	OwnerKey bool
	Client   client.Client
}

var _ KubernetesHelper = &DefaultKubernetesHelperImpl{}

func (r *DefaultKubernetesHelperImpl) execCommand(name string, arg []string, stdInData string) ([]byte, error) {
	command := exec.Command(name, arg...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if stdInData != "" {
		command.Stdin = strings.NewReader(stdInData)
	}
	result, err := command.Output()
	if err != nil {
		return nil, &ExecutionError{Msg: fmt.Sprintf("%+v.%s", stderr.String(), err)}
	}
	return result, nil
}

func (r *DefaultKubernetesHelperImpl) OpensslCommand(arg []string) ([]byte, error) {
	return r.execCommand("openssl", arg, "")
}

func (r *DefaultKubernetesHelperImpl) GetForceKey() bool {
	return r.ForceKey
}

func (r *DefaultKubernetesHelperImpl) GetOwnerKey() bool {
	return r.OwnerKey
}

func (r *DefaultKubernetesHelperImpl) GetConfigMap(name, namespace string) (*v1.ConfigMap, error) {
	cm := &v1.ConfigMap{}
	err := r.Client.Get(context.TODO(),
		types.NamespacedName{Name: name, Namespace: namespace}, cm)

	if err != nil {
		return cm, fmt.Errorf("Failed to get configmap %s, err: %v", name, err)
	}

	return cm, nil
}

func (r *DefaultKubernetesHelperImpl) ListPods(namespace string, labelSelectors map[string]string) (*v1.PodList, error) {
	podList := &v1.PodList{}

	listOps := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labelSelectors)},
	}
	err := r.Client.List(context.Background(), podList, listOps...)
	if err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *DefaultKubernetesHelperImpl) checkPodsCountByLabel(labelSelectors map[string]string, namespace string, numberOfPods int) (done bool, err error) {
	podList := &v1.PodList{}
	listOps := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labelSelectors)},
	}
	if err := r.Client.List(context.Background(), podList, listOps...); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return len(podList.Items) == numberOfPods, nil
}

func (r *DefaultKubernetesHelperImpl) WaitForPodsCountByLabel(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		return r.checkPodsCountByLabel(labelSelectors, namespace, numberOfPods)
	})
}

func (r *DefaultKubernetesHelperImpl) checkPodsByLabel(labelSelectors map[string]string, namespace string, numberOfPods int, podPhase v1.PodPhase, containerCheckFunc func(status v1.ContainerStatus) (bool, error)) (done bool, err error) {
	podList := &v1.PodList{}

	listOps := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labelSelectors)},
	}
	if err := r.Client.List(context.Background(), podList, listOps...); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if len(podList.Items) == numberOfPods {
		for _, pod := range podList.Items {
			if pod.Status.Phase == podPhase {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					result, err := containerCheckFunc(containerStatus)
					if err != nil {
						return false, err
					}
					if !result {
						return false, nil
					}
				}
			} else {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func (r *DefaultKubernetesHelperImpl) WaitForPodsCompleted(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		return r.checkPodsByLabel(labelSelectors, namespace, numberOfPods, v1.PodSucceeded, func(status v1.ContainerStatus) (bool, error) {
			terminated := status.State.Terminated
			if terminated != nil {
				if terminated.ExitCode == 0 {
					return true, nil
				} else {
					return false, &ExecutionError{
						Msg: fmt.Sprintf(
							"Pod's Container finished with non-zero exit code. Code: %v, Reason: %s, Message: %s",
							terminated.ExitCode, terminated.Reason, terminated.Message),
					}
				}
			} else {
				return false, nil
			}
		})
	})
}

func (r *DefaultKubernetesHelperImpl) WaitForPodsReady(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		return r.checkPodsByLabel(labelSelectors, namespace, numberOfPods, v1.PodRunning, func(status v1.ContainerStatus) (bool, error) {
			return status.Ready, nil
		})
	})
}

func (r *DefaultKubernetesHelperImpl) WaitForDeploymentReady(deployName string, namespace string, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		d := &v14.Deployment{}
		if err := r.Client.Get(context.Background(), types.NamespacedName{Name: deployName, Namespace: namespace}, d); err != nil {
			return false, err
		}
		if d.Status.ReadyReplicas != d.Status.Replicas {
			return false, nil
		}
		return true, nil
	})

}

func (r *DefaultKubernetesHelperImpl) WaitForTestsReady(deployName string, namespace string, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		dc := &v14.Deployment{}
		err = r.Client.Get(context.Background(), types.NamespacedName{Name: deployName, Namespace: namespace}, dc)

		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		for _, condition := range dc.Status.Conditions {
			if condition.Reason == "IntegrationTestsExecutionStatus" {
				if condition.Type == "Ready" || condition.Type == "Successful" {
					return true, nil
				} else if condition.Type == "In progress" {
					return false, nil
				} else if condition.Type == "Failed" {
					return false, goerrors.New(condition.Message)
				}
			}
		}
		return false, nil
	})

}

func (r *DefaultKubernetesHelperImpl) WaitForPVCBound(pvcName string, namespace string, waitSeconds int) error {
	return wait.PollImmediate(time.Second, time.Second*time.Duration(waitSeconds), func() (done bool, err error) {
		foundPvc := &v1.PersistentVolumeClaim{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pvcName, Namespace: namespace}, foundPvc); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if foundPvc.Status.Phase != v1.ClaimBound {
			return false, nil
		}

		return true, nil
	})
}

func newStringReader(ss []string) io.Reader {
	formattedString := strings.Join(ss, "\n")
	reader := strings.NewReader(formattedString)
	return reader
}

func (r *DefaultKubernetesHelperImpl) ExecRemote(log *zap.Logger, kubeConfig *rest.Config, podName string, namespace string, containerName string, command string, args []string) (string, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	kubeClient := kubernetes.NewForConfigOrDie(kubeConfig)

	restClient := kubeClient.CoreV1().RESTClient()

	execRequest := restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		Param("container", containerName).
		Param("command", command).
		Param("stdin", "true").
		Param("stdout", "true").
		Param("stderr", "true").
		Param("tty", "false").Timeout(time.Duration(1) * time.Minute)

	exec, err := remotecommand.NewSPDYExecutor(kubeConfig, "POST", execRequest.URL())
	if err != nil {
		return "", err
	}

	stdIn := newStringReader(args)

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdIn,
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})

	// log.Debug(fmt.Sprintf("Command output: %s", execOut.String()))
	if err != nil {
		return execOut.String(), fmt.Errorf("could not execute command %s: %v; stderr: %v, stdout: %s", command, err, execErr.String(), execOut.String())
	}

	if execErr.Len() > 0 {
		return execOut.String(), fmt.Errorf("stderr: %v", execErr.String())
	}

	return execOut.String(), nil
}

func (r *DefaultKubernetesHelperImpl) GetPodLogs(kubeConfig *rest.Config, podName string, namespace string, containerName string, tailLines *int64, previous bool) (string, error) {

	kubeClient := kubernetes.NewForConfigOrDie(kubeConfig)

	execRequest := kubeClient.CoreV1().Pods(namespace).GetLogs(
		podName,
		&v1.PodLogOptions{
			Container: containerName,
			TailLines: tailLines,
			Previous:  previous,
		},
	)

	resp, err := execRequest.DoRaw(context.Background())

	return string(resp), err
}

func (r *DefaultKubernetesHelperImpl) CreateRuntimeObject(scheme *runtime.Scheme, owner v12.Object, object client.Object, meta v12.ObjectMeta) error {
	if r.GetOwnerKey() {
		return CreateOrUpdateRuntimeObject(r.Client, scheme, owner, object, meta, r.GetForceKey())
	}

	return CreateOrUpdateRuntimeObject(r.Client, scheme, nil, object, meta, r.GetForceKey())
}

func (r *DefaultKubernetesHelperImpl) ListRuntimeObjectsByLabels(list client.ObjectList,
	namespace string, labelSelectors map[string]string) error {
	listOps := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labelSelectors)},
	}

	err := r.Client.List(context.Background(), list, listOps...)

	return err
}

func ListRuntimeObjectsByName(obj client.Object,
	kubeClient client.Client, namespace string, name string) error {
	err := kubeClient.Get(context.Background(), types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, obj)

	return err
}

func (r *DefaultKubernetesHelperImpl) DeleteDeploymentAndPods(dcName string, namespace string, waitSeconds int) error {
	oldDeployment := v14.Deployment{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Name: dcName, Namespace: namespace,
	}, &oldDeployment)
	if err != nil {
		if !errors.IsNotFound(err) {
			return &ExecutionError{Msg: "Error happened on checking " + dcName + " deployment. Error: " + err.Error()}
		}
	} else {
		err = DeleteRuntimeObjectWithCheck(r.Client, &oldDeployment, waitSeconds)
		if err != nil {
			return &ExecutionError{Msg: "Error happened on existed deployment deletion. Error: " + err.Error()}
		}

		err = r.WaitForPodsCountByLabel(
			oldDeployment.Spec.Template.ObjectMeta.Labels,
			namespace,
			0,
			waitSeconds)

		if err != nil {
			return &ExecutionError{Msg: "Error happened while waiting pods terminated. Error: " + err.Error()}
		}
	}

	return nil
}

func (r *DefaultKubernetesHelperImpl) DeleteStatefulsetAndPods(ssName string, namespace string, waitSeconds int) error {
	oldStatefulset := v14.StatefulSet{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Name: ssName, Namespace: namespace,
	}, &oldStatefulset)
	if err != nil {
		if !errors.IsNotFound(err) {
			return &ExecutionError{Msg: "Error happened on checking " + ssName + " statefulset. Error: " + err.Error()}
		}
	} else {
		err = DeleteRuntimeObjectWithCheck(r.Client, &oldStatefulset, waitSeconds)
		if err != nil {
			return &ExecutionError{Msg: "Error happened on existed statefulset deletion. Error: " + err.Error()}
		}

		err = r.WaitForPodsCountByLabel(
			oldStatefulset.Spec.Template.ObjectMeta.Labels,
			namespace,
			0,
			waitSeconds)

		if err != nil {
			return &ExecutionError{Msg: "Error happened while waiting pods terminated. Error: " + err.Error()}
		}
	}

	return nil
}

// TODO duplicate code as ^
func (r *DefaultKubernetesHelperImpl) DeleteRCAndPods(ssName string, namespace string, waitSeconds int) error {
	oldRc := v1.ReplicationController{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Name: ssName, Namespace: namespace,
	}, &oldRc)
	if err != nil {
		if !errors.IsNotFound(err) {
			return &ExecutionError{Msg: "Error happened on checking " + ssName + " replication controller. Error: " + err.Error()}
		}
	} else {
		err = DeleteRuntimeObjectWithCheck(r.Client, &oldRc, waitSeconds)
		if err != nil {
			return &ExecutionError{Msg: "Error happened on existed replication controller deletion. Error: " + err.Error()}
		}

		podList, err := r.ListPods(namespace, oldRc.Spec.Template.ObjectMeta.Labels)

		if err != nil {
			return &ExecutionError{Msg: "Error happened while retrieving mongos pods. Error: " + err.Error()}
		}

		for _, pod := range podList.Items {
			err = DeleteRuntimeObject(r.Client, &pod)
			if err != nil {
				return &ExecutionError{Msg: "Error happened while deleting mongos pods. Error: " + err.Error()}
			}
		}

		err = r.WaitForPodsCountByLabel(
			oldRc.Spec.Template.ObjectMeta.Labels,
			namespace,
			0,
			waitSeconds)

		if err != nil {
			return &ExecutionError{Msg: "Error happened while waiting pods terminated. Error: " + err.Error()}
		}
	}

	return nil
}

// TODO remove service name
func (r *DefaultKubernetesHelperImpl) GetDeploymentTypeByPVC(ctx ExecutionContext, serviceName string,
	pvcSelector map[string]string) (MicroServiceDeployType, error) {
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	pvcList := &v1.PersistentVolumeClaimList{}
	err := r.ListRuntimeObjectsByLabels(pvcList, request.Namespace, pvcSelector)
	var result MicroServiceDeployType
	if err != nil {
		result = Empty
	} else if len(pvcList.Items) == 0 {
		result = CleanDeploy
	} else {
		result = Update
	}

	if err == nil {
		log.Debug(fmt.Sprintf("%s deploy mode is used for %s service", result, serviceName))
	}

	return result, err
}

func (r *DefaultKubernetesHelperImpl) ScaleDeployment(obj *v14.Deployment, replicas int, timeout int) error {
	rep := int32(replicas)
	obj.Spec.Replicas = &rep
	return r.scale(obj, replicas, timeout, obj.Spec.Template.Labels, obj.Spec.Template.Namespace)
}

func (r *DefaultKubernetesHelperImpl) ScaleDeploymentByLabels(labels map[string]string, namespace string, replicas, timeout int) error {
	dl := &v14.DeploymentList{}
	err := r.ListRuntimeObjectsByLabels(dl, namespace, labels)
	if err != nil {
		return err
	}
	if len(dl.Items) == 0 {
		return NewNotFoundError(fmt.Sprintf("No deployment found by labels %v", labels))
	}
	return r.ScaleDeployment(&dl.Items[0], replicas, timeout)
}

func (r *DefaultKubernetesHelperImpl) UpdateDeploymentByLabels(labels map[string]string, namespace string, updateFunc func(depl *v14.Deployment)) error {
	dl := &v14.DeploymentList{}

	err := r.ListRuntimeObjectsByLabels(dl, namespace, labels)
	if err != nil {
		return err
	}
	if len(dl.Items) == 0 {
		return NewNotFoundError(fmt.Sprintf("No deployment found by labels %v", labels))
	}

	updateFunc(&dl.Items[0])

	return r.Client.Update(context.TODO(), &dl.Items[0], &client.UpdateOptions{})
}

func (r *DefaultKubernetesHelperImpl) ScaleStatefulset(obj *v14.StatefulSet, replicas, timeout int) error {
	rep := int32(replicas)
	obj.Spec.Replicas = &rep
	return r.scale(obj, replicas, timeout, obj.Spec.Template.Labels, obj.Spec.Template.Namespace)
}

func (r *DefaultKubernetesHelperImpl) ScaleReplicationController(obj *v1.ReplicationController, replicas int, timeout int) error {
	rep := int32(replicas)
	obj.Spec.Replicas = &rep
	return r.scale(obj, replicas, timeout, obj.Spec.Template.Labels, obj.Spec.Template.Namespace)
}

func (r *DefaultKubernetesHelperImpl) scale(obj client.Object, replicas int, timeout int, labels map[string]string, namespace string) error {
	err := r.Client.Patch(context.TODO(), obj, client.Merge, &client.PatchOptions{})
	if err != nil {
		return err
	}

	if replicas > 0 {
		err = r.WaitForPodsReady(
			labels,
			namespace,
			1,
			timeout)
		return err
	}
	return nil
}

func (r *DefaultKubernetesHelperImpl) RestartPod(pod *v1.Pod, namespace string, waitSeconds int) error {
	podDeleteTimeout := 5
	err := DeleteRuntimeObject(r.Client, pod)
	if err != nil {
		return fmt.Errorf("error while removal pod. Error: %v", err)
	}
	time.Sleep(time.Duration(podDeleteTimeout) * time.Second)

	err = r.WaitForPodsReady(
		pod.ObjectMeta.Labels,
		namespace,
		1,
		waitSeconds,
	)

	if err != nil {
		return fmt.Errorf("pod is not in ready state. Error: %v", err)
	}

	return nil
}

func ListRuntimeObjectsByNamespace(list client.ObjectList, cl client.Client, namespace string) error {
	listOps := []client.ListOption{
		client.InNamespace(namespace),
	}

	err := cl.List(context.Background(), list, listOps...)
	if err != nil {
		return err
	}
	return nil
}

func ReadSecret(kubeClient client.Client, name string, namespace string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	err := kubeClient.Get(context.TODO(),
		types.NamespacedName{Name: name, Namespace: namespace}, secret)

	return secret, err
}

func CompareSpecToCM(ctx ExecutionContext, cfgTemplate *v1.ConfigMap, spec interface{}, serviceName string) bool {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	storedResVersion := cfgTemplate.Data[serviceName]

	jsonBytes, jsonErr := json.Marshal(spec)
	if jsonErr != nil {
		log.Info(fmt.Sprintf("Failed to marshal spec to JSON, error: %v", jsonErr))
	}

	specHash := sha256.Sum256(jsonBytes)

	// Prevent recycling
	newResVersion := hex.EncodeToString(specHash[0:])
	log.Info(fmt.Sprintf("Current %s version is: %s "+
		"Stored %s Version is: %s", serviceName, newResVersion, serviceName, storedResVersion))

	if storedResVersion == newResVersion {
		log.Info(fmt.Sprintf("%s didn't change", serviceName))
		return false
	} else {
		if cfgTemplate.Data == nil {
			cfgTemplate.Data = map[string]string{
				serviceName: newResVersion,
			}
		} else {
			cfgTemplate.Data[serviceName] = newResVersion
		}

		return true
	}
}

func GetSpecConfigMap(ctx ExecutionContext) *v1.ConfigMap {
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	client := ctx.Get(constants.ContextClient).(client.Client)
	contextHashConfigMap := ctx.Get(constants.ContextHashConfigMap).(string)
	cm := &v1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Namespace: request.Namespace,
			Name:      contextHashConfigMap,
		},
	}
	err := client.Get(context.TODO(), types.NamespacedName{
		Name: cm.Name, Namespace: request.Namespace,
	}, cm)

	if err != nil && !errors.IsNotFound(err) {
		log.Error(fmt.Sprintf("Failed to get config map %s", contextHashConfigMap))
		panic(err)
	}

	return cm
}

func DeleteSpecConfigMap(ctx ExecutionContext) error {
	request := ctx.Get(constants.ContextRequest).(reconcile.Request)
	client := ctx.Get(constants.ContextClient).(client.Client)
	contextHashConfigMap := ctx.Get(constants.ContextHashConfigMap).(string)
	cm := &v1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Namespace: request.Namespace,
			Name:      contextHashConfigMap,
		},
	}

	return DeleteRuntimeObjectWithCheck(client, cm, 60)
}

func UpdateSpecConfigMap(ctx ExecutionContext, cm *v1.ConfigMap) error {
	client := ctx.Get(constants.ContextClient).(client.Client)
	scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)

	err := CreateOrUpdateRuntimeObject(
		client,
		scheme,
		nil,
		cm,
		cm.ObjectMeta,
		true)

	return err
}

func HasSpecChanged(ctx ExecutionContext, checkChanges func(cfgTemplate *v1.ConfigMap) bool) (bool, error) {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	kubeClient := ctx.Get(constants.ContextClient).(client.Client)
	scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)
	spec := ctx.Get(constants.ContextSpec).(client.Object)
	resultCheck := false

	cm := GetSpecConfigMap(ctx)

	resultCheck = checkChanges(cm)

	err := CreateOrUpdateRuntimeObjectAndWait(
		kubeClient,
		scheme,
		spec,
		cm,
		cm.ObjectMeta,
		true, true)
	if err != nil {
		log.Info("Can't store last reconcilation CR version. Error: " + err.Error())
	}

	return resultCheck, nil
}

func CheckSpecChange(ctx ExecutionContext, spec interface{}, serviceName string) (bool, error) {
	return HasSpecChanged(ctx, func(cfgTemplate *v1.ConfigMap) bool {
		return CompareSpecToCM(ctx, cfgTemplate, spec, serviceName)
	})
}

func DeleteRuntimeObjectWithCheck(cl client.Client, object client.Object, checkTimeout int) error {
	err := DeleteRuntimeObject(cl, object)

	if errors.IsNotFound(err) {
		return nil
	}

	zeroObject := Zero(object)
	emptyObject := (zeroObject).(client.Object)
	return wait.PollImmediate(time.Second, time.Second*time.Duration(checkTimeout), func() (done bool, err error) {
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name: object.GetName(), Namespace: object.GetNamespace(),
		}, emptyObject)

		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			} else {
				return false, err
			}
		}

		return false, nil
	})
}

// TODO looks like it should be in helper
func DeleteRuntimeObject(client client.Client, object client.Object) error {
	err := client.Delete(context.TODO(), object)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}
