package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/consul"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/vault"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CommonReconciler interface {
	UpdateStatus(condition types.ServiceStatusCondition)
	UpdateDRStatus(drStatus types.DisasterRecoveryStatus)
	GetStatus() *types.ServiceStatusCondition
	GetSpec() interface{}
	SetServiceInstance(client client.Client, request reconcile.Request)
	GetInstance() client.Object
	GetDeploymentVersion() string
	GetVaultRegistration() *types.VaultRegistration
	GetConsulRegistration() *types.ConsulRegistration
	GetConsulServiceRegistrations() map[string]*types.AgentServiceRegistration
	GetMessage() string
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

func (r *ReconcileCommonService) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result, executionErrResult error) {
	logger := GetLogger(getEnvAsBool("DEBUG_LOG", true))

	var consulClient consul.ConsulClient
	var consulErr error

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
					updateDRStatus(r.Reconciler, r.Client, "failed")
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
			updateInstanceStatus(r.Reconciler, r.Client, true, "Failed", executionErrResult, "ReconcileCycleFailed")
			updateDRStatus(r.Reconciler, r.Client, "failed")
		}

		//if consulClient != nil {
		//	consulLogoutErr := consulClient.Logout()
		//	HandleError(consulLogoutErr, logger.Warn, "Consul client failed to logout")
		//}
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
	})

	deploymentVersion := getEnv("DEPLOYMENT_VERSION", "")

	if deploymentVersion != "" {
		crDeploymentVersion := r.Reconciler.GetDeploymentVersion()
		logger.Debug(fmt.Sprintf("Stored deployment version: %s . Current CR deployment version: %s", deploymentVersion, crDeploymentVersion))
		if deploymentVersion != crDeploymentVersion {
			sleepTime := getEnvAsInt("DEPLOYMENT_VERSION_MISMATCH_SLEEP_SECONDS", 5*60)
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

	specHasChanges, specCheckErr := CheckSpecChange(deploymentContext, r.Reconciler.GetSpec(), "spec-summary")

	if specCheckErr != nil {
		logger.Warn("CR Spec Hash checking error: " + specCheckErr.Error())
	}

	if !specHasChanges && !isCurrentStatus(r.Reconciler, "Successful") {
		logger.Debug("Spec has no changes and current status is not Successful")
		logger.Debug(fmt.Sprintf("Message: %s", r.Reconciler.GetMessage()))
		return
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
		updateInstanceStatus(r.Reconciler, r.Client, true, "In Progress", nil, "ReconcileCycleInProgress")
		updateDRStatus(r.Reconciler, r.Client, "running")

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
			updateInstanceStatus(r.Reconciler, r.Client, true, "Successful", nil, "ReconcileCycleSucceeded")
			logger.Info("Reconcile cycle succeeded")

			if r.DRBuilder != nil {
				r.Executor.SetExecutable(r.DRBuilder.Build(deploymentContext))
				executionErrResult = r.Executor.Execute(deploymentContext)
				updateDRStatus(r.Reconciler, r.Client, "done")
			}
		}

	}

	return
}

func updateInstanceStatus(reconciler CommonReconciler, client client.Client, conditionStatus bool, statusType string, err error, reason string) {
	transitionTime := v12.Time{Time: time.Now()}
	conditionMsg := ""

	if err != nil {
		conditionMsg = err.Error()
	}

	condition := types.ServiceStatusCondition{
		Type:               statusType,
		Status:             conditionStatus,
		LastTransitionTime: transitionTime,
		Reason:             reason,
		Message:            strings.ReplaceAll(conditionMsg, "\t", " "),
	}
	reconciler.UpdateStatus(condition)

	conditionUpdateErr := client.Status().Update(context.TODO(), reconciler.GetInstance())
	if conditionUpdateErr != nil {
		logger := GetLogger(getEnvAsBool("DEBUG_LOG", true))
		logger.Error("Cannot update CR conditions. Error: " + conditionUpdateErr.Error())
	}
}

func updateDRStatus(reconciler CommonReconciler, client client.Client, status string) {
	drStatus := types.DisasterRecoveryStatus{
		Status: status,
	}
	reconciler.UpdateDRStatus(drStatus)
	//TODO DR cleanup
	drStatusUpdateErr := client.Status().Update(context.TODO(), reconciler.GetInstance())
	if drStatusUpdateErr != nil {
		logger := GetLogger(getEnvAsBool("DEBUG_LOG", true))
		logger.Error("Cannot update DR Status. Error: " + drStatusUpdateErr.Error())
	}
}

func isCurrentStatus(reconciler CommonReconciler, statusType string) bool {
	statusConditions := reconciler.GetStatus()
	if statusConditions != nil {
		return statusConditions.Type == statusType
	}

	return false
}
