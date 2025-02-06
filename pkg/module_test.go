package pkg

import (
	"context"
	"fmt"
	"testing"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	mFake "github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/fake"
	mTypes "github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/vault"
	"github.com/docker/distribution/uuid"
	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"gotest.tools/assert"
	v1apps "k8s.io/api/apps/v1"
	v1core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Vaulter interface {
	VaultGetToken() string
	GetClient() *api.Client
	VaultGeneratePasswordWithPolicy(policyName string, client *api.Client) (string, error)
	VaultCreatePasswordPolicy(policyName string, policy string, client *api.Client) error
	VaultWrite(path string, secret map[string]interface{}, client *api.Client) error
	VaultRead(path string, client *api.Client) (map[string]interface{}, error)
}

type FakeVaultImpl struct {
	t        *testing.T
	password string
}

func (r *FakeVaultImpl) GetClient() *api.Client {
	return nil
}

func (r *FakeVaultImpl) VaultGeneratePasswordWithPolicy(policyName string, client *api.Client) (string, error) {
	return r.password, nil
}

func (r *FakeVaultImpl) VaultCreatePasswordPolicy(policyName string, policy string, client *api.Client) error {
	return nil
}
func (r *FakeVaultImpl) VaultRead(path string, client *api.Client) (map[string]interface{}, error) {
	return nil, nil
}

func (r *FakeVaultImpl) VaultWrite(path string, secret map[string]interface{}, client *api.Client) error {
	return nil
}

type TestUtilsImpl struct {
	core.DefaultKubernetesHelperImpl
}

var _ core.KubernetesHelper = &TestUtilsImpl{}

func (r *TestUtilsImpl) WaitForPVCBound(pvcName string, namespace string, waitSeconds int) error {
	return nil
}

func (r *TestUtilsImpl) WaitForDeploymentReady(deployName string, namespace string, waitSeconds int) error {
	return nil
}

func (r *TestUtilsImpl) WaitForPodsReady(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return nil
}

func (r *TestUtilsImpl) WaitForPodsCompleted(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return nil
}

func (r *TestUtilsImpl) WaitPodsCountByLabel(labelSelectors map[string]string, namespace string, numberOfPods int, waitSeconds int) error {
	return nil
}

func (r *TestUtilsImpl) ExecRemote(log *zap.Logger, kubeConfig *rest.Config, podName string, namespace string, containerName string, command string, args []string) (string, error) {
	return "", nil
}

func (r *TestUtilsImpl) GetPodLogs(kubeConfig *rest.Config, podName string, namespace string, containerName string, tailLines *int64, previous bool) (string, error) {

	return "", nil
}

func (r *TestUtilsImpl) ListPods(namespace string, labelSelectors map[string]string) (*v1core.PodList, error) {
	return &v1core.PodList{
		Items: []v1core.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod",
					Namespace: namespace,
					Labels:    labelSelectors,
				},
				Spec: v1core.PodSpec{
					Containers: []v1core.Container{
						{
							Name: "pod",
						},
					},
				},
			},
		},
	}, nil
}

func generatePV(nameFormat string, nodeFormat string, namespace string, size int) ([]runtime.Object, []string, []map[string]string, []string) {
	pvSize := []string{}
	pvS := []runtime.Object{}
	names := []string{}
	nodeLabels := []map[string]string{}
	for i := 1; i <= size; i++ {
		pvS = append(pvS, &v1core.PersistentVolume{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(nameFormat, i),
				Namespace: namespace,
				Labels: map[string]string{
					"node": fmt.Sprintf(nodeFormat, i),
				},
			},
		})
		names = append(names, fmt.Sprintf(nameFormat, i))
		nodeLabels = append(nodeLabels, map[string]string{constants.KubeHostName: fmt.Sprintf(nodeFormat, i)})
		pvSize = append(pvSize, "5Gi")
	}

	return pvS, names, nodeLabels, pvSize
}
func generateDCPV(nameFormat string, nodeFormat string, namespace string, dcIndex, size int) ([]runtime.Object, []string, []map[string]string) {
	pvS := []runtime.Object{}
	names := []string{}
	nodeLabels := []map[string]string{}
	for i := 0; i < size; i++ {
		index := dcIndex*size + i
		pvS = append(pvS, &v1core.PersistentVolume{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(nameFormat, dcIndex, i),
				Namespace: namespace,
				Labels: map[string]string{
					constants.KubeHostName: fmt.Sprintf(nodeFormat, index),
				},
			},
		})
		names = append(names, fmt.Sprintf(nameFormat, dcIndex, i))
		nodeLabels = append(nodeLabels, map[string]string{constants.KubeHostName: fmt.Sprintf(nodeFormat, index)})
	}

	return pvS, names, nodeLabels
}

func GenerateDefaultFake(namespace string, fakePvName []string, fakeNodeLabels []map[string]string, fakePvcSize []string) *mFake.FakeService {
	var fsGroup int64 = 999
	var tolerationSeconds int64 = 20
	GiQuantity, _ := resource.ParseQuantity("5Gi")
	rr := &v1core.ResourceRequirements{
		Limits: v1core.ResourceList{
			v1core.ResourceMemory: GiQuantity,
		},
		Requests: nil,
	}
	return &mFake.FakeService{
		Spec: mFake.FakeSpec{
			Storage: &mTypes.StorageRequirements{
				Size:       fakePvcSize,
				Volumes:    fakePvName,
				NodeLabels: fakeNodeLabels,
			},
			Resources: rr,
			Policies: &mFake.Policies{
				Tolerations: []v1core.Toleration{
					{
						Key:               "key1",
						Value:             "value1",
						Operator:          v1core.TolerationOpEqual,
						Effect:            v1core.TaintEffectNoSchedule,
						TolerationSeconds: &tolerationSeconds,
					},
					{
						Key:               "key2",
						Value:             "value2",
						Operator:          v1core.TolerationOpEqual,
						Effect:            v1core.TaintEffectNoExecute,
						TolerationSeconds: &tolerationSeconds,
					},
				},
			},
			PodSecurityContext: &v1core.PodSecurityContext{
				FSGroup: &fsGroup,
			},
			VaultRegistration: mTypes.VaultRegistration{
				Enabled:                true,
				Path:                   "secret",
				InitContainerResources: rr,
			},
		},
	}
}

type CaseStruct struct {
	name                          string
	nameSpace                     string
	executor                      core.Executor
	builder                       core.ExecutableBuilder
	ctx                           core.ExecutionContext
	ctxToReplaceAfterServiceBuilt map[string]interface{}
	RunTestFunc                   func() error
	ReadResultFunc                func(t *testing.T, err error)
}

func GenerateDefaultTestCase(
	testName string,
	fakeServiceSpec *mFake.FakeService,
	runtimeObjects []runtime.Object,
	nameSpace string,
	nameSpaceRequestName string,
	vaultImpl vault.VaultHelper,
) CaseStruct {

	utilsHelp := &TestUtilsImpl{}
	utilsHelp.ForceKey = true
	// Because there is empty runtime Scheme
	utilsHelp.OwnerKey = false
	client := fake.NewFakeClient(runtimeObjects...)
	utilsHelp.Client = client
	caseStruct := CaseStruct{
		name:      testName,
		nameSpace: nameSpace,
		executor:  core.DefaultExecutor(),
		builder:   &mFake.FakeServiceBuilder{},
		ctx: core.GetExecutionContext(map[string]interface{}{
			constants.ContextSpec:   fakeServiceSpec,
			constants.ContextSchema: &runtime.Scheme{},
			constants.ContextRequest: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: nameSpace,
					Name:      nameSpaceRequestName,
				},
			},
			constants.ContextClient:                client,
			constants.ContextKubeClient:            &rest.Config{},
			constants.ContextLogger:                core.GetLogger(true),
			"contextResourceOwner":                 fakeServiceSpec,
			constants.ContextServiceDeploymentInfo: map[string]string{},
			constants.ContextVault:                 vaultImpl,
			constants.KubernetesHelperImpl:         utilsHelp,
		}),

		ReadResultFunc: func(t *testing.T, err error) {
			if err != nil {
				t.Error(err)
			}
		},
	}

	return caseStruct
}

func GenerateDefaultServiceWrapper(testName string, vaultImpl vault.VaultHelper) CaseStruct {

	nameSpace := "fake-namespace"
	nameSpaceRequestName := "fake-name"
	fakePv, fakeName, fakeNodes, fakePvcSize := generatePV("fake-data-%v", "node-%v", nameSpaceRequestName, 1)

	fakeService := GenerateDefaultFake(
		nameSpace,
		fakeName,
		fakeNodes,
		fakePvcSize,
	)

	return GenerateDefaultTestCase(
		testName,
		fakeService,
		fakePv,
		nameSpace,
		nameSpaceRequestName,
		vaultImpl,
	)
}

func TestExecutionCheck(t *testing.T) {
	pass := uuid.Generate().String()
	vaultImpl := vault.FakeVaultHelper{}
	testFuncs := []func() CaseStruct{
		func() CaseStruct {
			vaultImpl.On("CheckSecretExists", "fakeSecretName").Return(false, make(map[string]interface{}), nil)
			vaultImpl.On("GeneratePassword", mock.Anything).Return(pass, nil)
			vaultImpl.On("StorePassword", mock.Anything, mock.Anything).Return(nil)
			cs := GenerateDefaultServiceWrapper("One DC All Services", vaultImpl)
			cs.executor.SetExecutable(cs.builder.Build(cs.ctx))
			cs.ReadResultFunc = func(t *testing.T, err error) {
				password := cs.ctx.Get("password").(string)
				assert.Equal(t, pass, password)
			}

			cs.RunTestFunc = func() error {
				return cs.executor.Execute(cs.ctx)
			}
			return cs
		},
		func() CaseStruct {
			cs := GenerateDefaultServiceWrapper("One DC All Services already existing vault secret", vaultImpl)
			cs.executor.SetExecutable(cs.builder.Build(cs.ctx))
			cs.ReadResultFunc = func(t *testing.T, err error) {
				password := cs.ctx.Get("password").(string)
				assert.Equal(t, pass, password)
			}
			cs.RunTestFunc = func() error {
				return cs.executor.Execute(cs.ctx)
			}
			return cs
		},
		func() CaseStruct {
			cs := GenerateDefaultServiceWrapper("Check 2 replicas", vaultImpl)
			cs.executor.SetExecutable(cs.builder.Build(cs.ctx))
			cs.ReadResultFunc = func(t *testing.T, err error) {
				dc := &v1apps.Deployment{}
				client := cs.ctx.Get(constants.ContextClient).(client.Client)
				request := cs.ctx.Get(constants.ContextRequest).(reconcile.Request)
				err = client.Get(context.TODO(),
					types.NamespacedName{Name: "Fake", Namespace: request.Namespace}, dc)
				if err != nil {
					t.Error(err)
				}
				var expected int32 = 2
				assert.DeepEqual(t, dc.Spec.Replicas, &expected)
			}
			cs.RunTestFunc = func() error {
				return cs.executor.Execute(cs.ctx)
			}
			return cs
		},
	}

	tests := []CaseStruct{}
	for _, tf := range testFuncs {
		tests = append(tests, tf())
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, elem := range tt.ctxToReplaceAfterServiceBuilt {
				tt.ctx.Set(key, elem)
			}
			err := tt.RunTestFunc()
			tt.ReadResultFunc(t, err)
		})
	}
}
