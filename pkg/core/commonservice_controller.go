package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/Netcracker/qubership-credential-manager/pkg/informer"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/consul"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/vault"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CommonReconciler interface {
	UpdateStatus(condition types.ServiceStatusCondition)
	UpdateDRStatus(drStatus types.DisasterRecoveryStatus)
	GetStatus() *types.ServiceStatusCondition
	GetSpec() interface{}
	GetConfigMapName() string
	SetServiceInstance(client client.Client, request reconcile.Request)
	GetInstance() client.Object
	GetDeploymentVersion() string
	GetVaultRegistration() *types.VaultRegistration
	GetConsulRegistration() *types.ConsulRegistration
	GetConsulServiceRegistrations() map[string]*types.AgentServiceRegistration
	GetMessage() string
	UpdatePassword() Executable
	GetAdminSecretName() string
	UpdatePassWithFullReconcile() bool
}

type DefaultCommonReconciler struct {
	CommonReconciler
}

type ReconcileCommonService struct {
	Client     client.Client
	KubeConfig *rest.Config
	Scheme     *runtime.Scheme
	Executor   Executor
	// PredeployBuilder Doesn't take into account CR changes
	// PredeployBuilder Runs before Builder
	PredeployBuilder ExecutableBuilder
	DRBuilder        ExecutableBuilder
	Builder          ExecutableBuilder
	DREnabled        bool
	Reconciler       CommonReconciler
}

func (r *ReconcileCommonService) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result, reconcileError error) {
	logger := GetLogger(getEnvAsBool("DEBUG_LOG", true))

	crHandler := DefaultCRStatusHandler{
		Reconciler: r.Reconciler,
		KubeClient: r.Client,
	}

	var consulClient consul.ConsulClient
	var consulErr error

	//we always return error == nil, this way we implement our own logic of reconcile retries
	var executionErrResult error

	defer func() {
		panicStackTrace := string(debug.Stack())
		var errMsg string
		if err := recover(); err != nil {
			// Panic
			var stringErrorMsg string
			switch v := err.(type) {
			case string:
				stringErrorMsg = v
			case error:
				var dre *DRExecutionError
				if errors.As(err.(error), &dre) {
					statusErr := crHandler.SetDRStatus("failed").Commit()
					if statusErr != nil {
						logger.Sugar().Errorf("Failed to update DR status to 'failed', err: %v", statusErr)
					}
					logger.Error(v.Error() + "\n" + panicStackTrace)
					return
				}
				stringErrorMsg = v.Error()
			}

			errMsg = stringErrorMsg + "\n" + panicStackTrace
		} else if executionErrResult != nil {
			// Usual exception
			errMsg = executionErrResult.Error()
		}

		if errMsg != "" {
			resultMsg := "Reconciliation exception: " + errMsg
			logger.Error(resultMsg)
			executionErrResult = &ExecutionError{Msg: resultMsg}

			statusErr := crHandler.SetCRCondition(true, "Failed", executionErrResult, "ReconcileCycleFailed").SetDRStatus("failed").Commit()
			if statusErr != nil {
				logger.Sugar().Errorf("Failed to update CR status, err: %v", statusErr)
			}
		}

	}()

	r.Reconciler.SetServiceInstance(r.Client, request)
	deploymentContext := GetExecutionContext(map[string]interface{}{
		constants.ContextSpec:                       r.Reconciler.GetInstance(),
		constants.ContextSchema:                     r.Scheme,
		constants.ContextRequest:                    request,
		constants.ContextClient:                     r.Client,
		constants.ContextKubeClient:                 r.KubeConfig,
		constants.ContextLogger:                     logger,
		constants.ContextVault:                      vault.NewVaulterHelperImpl(vault.NewVaultClientImpl(r.Reconciler.GetVaultRegistration())),
		constants.ContextConsulRegistration:         r.Reconciler.GetConsulRegistration(),
		constants.ContextConsulServiceRegistrations: r.Reconciler.GetConsulServiceRegistrations(),
		constants.ContextHashConfigMap:              r.Reconciler.GetConfigMapName(),
	})

	deploymentVersion := getEnv("DEPLOYMENT_VERSION", "")
	sleepTime := getEnvAsInt("DEPLOYMENT_VERSION_MISMATCH_SLEEP_SECONDS", 5*60)

	if deploymentVersion != "" {
		crDeploymentVersion := r.Reconciler.GetDeploymentVersion()
		logger.Debug(fmt.Sprintf("Stored deployment version: %s . Current CR deployment version: %s", deploymentVersion, crDeploymentVersion))
		if deploymentVersion != crDeploymentVersion {

			logger.Info(fmt.Sprintf("Deployment version mismatch. Sleeping for %v seconds and exit...", sleepTime))
			time.Sleep(time.Duration(sleepTime) * time.Second)
			os.Exit(0)
		}
	}

	delay := getEnvAsInt("RECONCILIATION_DELAY_SECONDS", 0)
	if delay > 0 {
		logger.Info(fmt.Sprintf("Delayed execution: %v seconds...", delay))
		time.Sleep(time.Duration(delay) * time.Second)
	}

	// deploymentVersionChanged, _ := CheckSpecChange(deploymentContext, deploymentVersion, "deploymentVersion")
	specHasChanges, specCheckErr := CheckSpecChange(deploymentContext, r.Reconciler.GetSpec(), "spec-summary")

	if specCheckErr != nil {
		logger.Warn("CR Spec Hash checking error: " + specCheckErr.Error())
	}

	adminSecret := r.Reconciler.GetAdminSecretName()
	if adminSecret != "" {
		err := informer.Watch([]string{r.Reconciler.GetAdminSecretName()}, func() {

			if r.Reconciler.UpdatePassWithFullReconcile() {

				err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Duration(sleepTime)*time.Second, true,
					func(ctx context.Context) (done bool, err error) {
						changed, err := manager.AreCredsChanged([]string{r.Reconciler.GetAdminSecretName()})
						return !changed && err == nil, nil
					})

				if err != nil {
					logger.Error(fmt.Sprintf("Failed to wait secret and secret old is identical, err: %v", err))
				}

				resetErr := doResetSpec(deploymentContext)
				if resetErr != nil {
					logger.Error(fmt.Sprintf("Failed to reset spec config map, err: %v", resetErr))
				}
				r.Reconcile(ctx, request)
			} else {
				updateErr := r.Reconciler.UpdatePassword().Execute(deploymentContext)
				if updateErr != nil {
					logger.Error(fmt.Sprintf("Failed to update password, err: %v", updateErr))
				} else {
					logger.Info("Password updated")
				}
			}
		})

		if err != nil {
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}

		areCredsChanged, _ := manager.AreCredsChanged([]string{r.Reconciler.GetAdminSecretName()})

		if areCredsChanged {
			specHasChanges = true
		} else {
			// no changes in secret - need to unlock secret
			err := manager.ActualizeCreds(r.Reconciler.GetAdminSecretName(), func(newSecret, oldSecret *v1.Secret) error {
				return nil
			})

			if err != nil {
				logger.Error(fmt.Sprintf("Failed to actualize secret %s, err: %v", r.Reconciler.GetAdminSecretName(), err))
			}
		}

	}

	if specHasChanges && !isCurrentStatus(r.Reconciler, "Successful") {
		logger.Info(fmt.Sprintf(`Looks like the last deploy has failed and this is a new one. 
			Continue with deleted %v config map to run full reconcile.`, r.Reconciler.GetConfigMapName()))

		if r.Reconciler.GetMessage() != "" {
			// logger.Info(fmt.Sprintf("Message: %s", r.Reconciler.GetMessage()))
		}

		out, _ := json.Marshal(r.Reconciler.GetSpec())
		logger.Info(fmt.Sprintf("CR:\n%s", string(out)))

		reconcileError = doResetSpec(deploymentContext)
		CheckSpecChange(deploymentContext, r.Reconciler.GetSpec(), "spec-summary")
		if reconcileError != nil {
			return
		}
	}

	deploymentContext.Set(constants.ContextSpecHasChanges, specHasChanges)

	if r.PredeployBuilder != nil {
		logger.Info("Performing pre-deploy...")
		r.Executor.SetExecutable(r.PredeployBuilder.Build(deploymentContext))
		//error will be catched by defer above
		executionErrResult = r.Executor.Execute(deploymentContext)
		logger.Info("Pre-deploy is finished")
	}

	if specHasChanges && executionErrResult == nil {
		statusErr := crHandler.SetCRCondition(true, "In Progress", nil, "ReconcileCycleInProgress").SetDRStatus("running").Commit()
		if statusErr != nil {
			logger.Sugar().Errorf("Failed to update CR status, err: %v", statusErr)
		}

		nodeIP := getEnv("HOST_IP", "")
		//consulClient, consulErr = consul.NewConsulClientImpl(nodeIP, request.Namespace, r.Reconciler.GetConsulRegistration(), r.KubeConfig, logger)
		consulClient, consulErr = consul.NewConsulClientImpl(nodeIP, "", r.Reconciler.GetConsulRegistration(), r.KubeConfig, logger)
		PanicError(consulErr, logger.Error, "Error is happened during consul client creation")
		deploymentContext.Set(constants.ContextConsul, consulClient)

		r.Executor.SetExecutable(r.Builder.Build(deploymentContext))

		logger.Info("Reconciliation starts...")

		//error will be catched by defer above
		executionErrResult = r.Executor.Execute(deploymentContext)
		if executionErrResult == nil {
			// Update last success execution version
			statusErr := crHandler.SetCRCondition(true, "Successful", nil, "ReconcileCycleSucceeded").Commit()
			if statusErr != nil {
				logger.Sugar().Errorf("Failed to update CR status, err: %v", statusErr)
			}
			logger.Info("Reconcile cycle succeeded")

			if r.DRBuilder != nil {
				r.Executor.SetExecutable(r.DRBuilder.Build(deploymentContext))
				executionErrResult = r.Executor.Execute(deploymentContext)

				statusErr := crHandler.SetDRStatus("done").Commit()
				if statusErr != nil {
					logger.Sugar().Errorf("Failed to update CR status, err: %v", statusErr)
				}
			}
		}

	}
	return
}

func isCurrentStatus(reconciler CommonReconciler, statusType string) bool {
	statusConditions := reconciler.GetStatus()
	if statusConditions != nil {
		return statusConditions.Type == statusType
	}

	return false
}

func doResetSpec(deploymentContext ExecutionContext) error {
	err := DeleteSpecConfigMap(deploymentContext)
	if err != nil {
		return fmt.Errorf("Failed to delete Spec config map, err: %w", err)
	}

	return nil
}
