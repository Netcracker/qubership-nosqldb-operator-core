package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/consul"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/utils"
	"github.com/docker/distribution/uuid"
	consulApi "github.com/hashicorp/consul/api"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ConsulSettingsWrapperExecutionFunction func(ctx core.ExecutionContext, client consul.ConsulClient, srEnabled bool, proxyChecksHosts bool, serviceRegistration *consulApi.AgentServiceRegistration, logger *zap.Logger) error
type ConsulSettingsWrapperCastServiceRegistrationFunction func(ctx core.ExecutionContext, client consul.ConsulClient, registration *types.AgentServiceRegistration, logger *zap.Logger) *consulApi.AgentServiceRegistration
type ConsulSettingsWrapperAdditionalConditionFunc func(ctx core.ExecutionContext, client consul.ConsulClient, registration *types.AgentServiceRegistration, logger *zap.Logger) (bool, error)

const consulSettingsHashKey = "%s-consul-settings-hash"
const ConsulRegistrationHashKey = "consul-registration-hash"

const checkServiceNameFormat = "consul-check-%s-%s-%s"
const consulCheckLabelKey = "consul-check-proxy"

type ConsulSettingsWrapperStep struct {
	core.DefaultExecutable
	// Step name. Just for logging
	Name string
	// Settings section name from values.yaml
	ConsulSettingsName string
	// Custom casting to AgentServiceRegistration
	CastServiceRegistrationFunc ConsulSettingsWrapperCastServiceRegistrationFunction
	ExecuteFunc                 ConsulSettingsWrapperExecutionFunction
	AdditionalConditionFunc     ConsulSettingsWrapperAdditionalConditionFunc
	//
	SkipHasChangesCheck bool
}

func (r *ConsulSettingsWrapperStep) Execute(ctx core.ExecutionContext) error {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	consulClient := ctx.Get(constants.ContextConsul).(consul.ConsulClient)

	log.Debug(fmt.Sprintf("Consul step %s is started", r.Name))
	defer log.Debug(fmt.Sprintf("Consul step %s is ended", r.Name))

	sr := FindServiceRegistrationByKey(ctx, r.ConsulSettingsName)
	if sr != nil {
		castedSR := r.CastAndUpdateConsulServiceRegistration(ctx, consulClient, sr, log)
		return r.ExecuteFunc(ctx, consulClient, sr.Enabled, !sr.DirectChecks, castedSR, log)
	} else {
		panic(fmt.Sprintf("Service Registration settings not found for '%s'", r.ConsulSettingsName))
	}
}

func (r *ConsulSettingsWrapperStep) Condition(ctx core.ExecutionContext) (bool, error) {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)

	if ctx.Get(constants.ContextConsulRegistration) == nil ||
		ctx.Get(constants.ContextConsul) == nil {
		log.Debug("Consul client is not set. Skipping step...")
		return false, nil
	}

	consulRegistration := ctx.Get(constants.ContextConsulRegistration).(*types.ConsulRegistration)
	consulClient := ctx.Get(constants.ContextConsul).(consul.ConsulClient)

	if len(r.ConsulSettingsName) != 0 {
		sr := FindServiceRegistrationByKey(ctx, r.ConsulSettingsName)
		if sr == nil {
			log.Warn(fmt.Sprintf("Service Registration settings not found for '%s'. Skipping...", r.ConsulSettingsName))
			return false, nil
		}

		hashKey := fmt.Sprintf(consulSettingsHashKey, r.ConsulSettingsName)
		contextSettingsHashValue := ctx.Get(hashKey)

		contextRegistrationHashValue := ctx.Get(ConsulRegistrationHashKey)

		hasChanges := false

		if contextSettingsHashValue != nil {
			// For reconciliation loop
			hasChanges = contextSettingsHashValue.(bool)
		} else {
			var hashCheckErr error
			hasChanges, hashCheckErr = core.CheckSpecChange(ctx, sr, hashKey)
			if hashCheckErr != nil {
				return false, hashCheckErr
			}
			ctx.Set(hashKey, hasChanges)
			//To prevent simultaneous changes to hash configmap
			time.Sleep(1 * time.Second)
		}

		if contextRegistrationHashValue != nil {
			hasChanges = hasChanges || contextRegistrationHashValue.(bool)
		} else {
			regChanges, regHashCheckErr := core.CheckSpecChange(ctx, consulRegistration, ConsulRegistrationHashKey)
			if regHashCheckErr != nil {
				return false, regHashCheckErr
			}
			ctx.Set(ConsulRegistrationHashKey, regChanges)
			hasChanges = hasChanges || regChanges
		}

		if !hasChanges {
			if !r.SkipHasChangesCheck {
				log.Debug("Nothing has changed in consul settings for " + r.ConsulSettingsName + ". Skipping...")
				return false, nil
			} else {
				log.Debug("Skipping " + r.ConsulSettingsName + " consul settings changes check...")
			}
		}

		if r.AdditionalConditionFunc != nil {
			return r.AdditionalConditionFunc(ctx, consulClient, sr, log)
		}
	} else {
		return false, &core.ExecutionError{Msg: "Service Registration settings name is not set"}
	}

	return true, nil
}

func NewRegisterConsulServiceStep(
	settingsName string,
	castFunc ConsulSettingsWrapperCastServiceRegistrationFunction) *ConsulSettingsWrapperStep {
	return &ConsulSettingsWrapperStep{
		Name:                        fmt.Sprintf("%s registration/deregistration", settingsName),
		ConsulSettingsName:          settingsName,
		CastServiceRegistrationFunc: castFunc,
		ExecuteFunc: func(ctx core.ExecutionContext, client consul.ConsulClient, srEnabled bool, proxyChecksHosts bool, serviceRegistration *consulApi.AgentServiceRegistration, logger *zap.Logger) error {
			kubeCl := ctx.Get(constants.ContextClient).(kubeClient.Client)
			request := ctx.Get(constants.ContextRequest).(reconcile.Request)
			scheme := ctx.Get(constants.ContextSchema).(*runtime.Scheme)
			helperImpl := ctx.Get(constants.KubernetesHelperImpl).(core.KubernetesHelper)

			serviceLabels := map[string]string{consulCheckLabelKey: settingsName}

			//Remove existed service proxy checks
			logger.Debug("Removing existed proxy services for consul checks")
			servicesList := &v1.ServiceList{}
			listOps := []kubeClient.ListOption{
				kubeClient.InNamespace(request.Namespace),
				kubeClient.MatchingLabelsSelector{
					Selector: labels.SelectorFromSet(serviceLabels),
				},
			}

			if err := kubeCl.List(context.Background(), servicesList, listOps...); err != nil {
				if errors.IsNotFound(err) {
					logger.Debug("Services to delete not found")
				} else {
					core.HandleError(err, logger.Error, "Failed listing consul checks proxy services")
				}
			} else {
				for _, service := range servicesList.Items {
					logger.Debug("Removing service: " + service.Name)
					core.HandleError(
						core.DeleteRuntimeObject(kubeCl, &service),
						logger.Error,
						"Failed removing service: "+service.Name)
				}
			}

			if !srEnabled {
				logger.Debug(fmt.Sprintf("Performing service %s deregistration in consul...", serviceRegistration.ID))
				deregErr := client.Deregister(serviceRegistration.ID)
				core.HandleError(deregErr, logger.Warn, "Failed during service deregistration")
				return nil
			} else {
				logger.Debug(fmt.Sprintf("Performing service %s registration in consul...", serviceRegistration.ID))

				if proxyChecksHosts {
					for _, check := range serviceRegistration.Checks {
						// Generate new service with random name
						randForService := strings.Split(uuid.Generate().String(), "-")[0]
						newServiceName := fmt.Sprintf(checkServiceNameFormat, settingsName, check.Name, randForService)
						newServiceHost := fmt.Sprintf(constants.ServiceClusterDomainTemplate, newServiceName, request.Namespace)
						hostToProxy := strings.Split(check.TCP, ":")

						logger.Debug(fmt.Sprintf("Proxying checking of %s via %s host...", hostToProxy[0], newServiceHost))
						service := utils.GetProxyService(
							newServiceName,
							request.Namespace,
							serviceLabels,
							hostToProxy[0])

						// create service
						core.PanicError(
							helperImpl.CreateRuntimeObject(scheme, nil, service, service.ObjectMeta),
							logger.Error,
							"Failed creating proxy service for consul check")

						//if port is presented
						if len(hostToProxy) > 1 {
							check.TCP = newServiceHost + ":" + hostToProxy[1]
						} else {
							check.TCP = newServiceHost
						}
						logger.Debug(fmt.Sprintf("Replaced TCP check with: %s", check.TCP))
					}
				}

				return client.Register(serviceRegistration)
			}
		},
	}
}

func NewMaintenanceConsulServiceStep(
	settingsName string,
	castFunc ConsulSettingsWrapperCastServiceRegistrationFunction,
	isMainteaning bool,
	reason ...string) *ConsulSettingsWrapperStep {
	return &ConsulSettingsWrapperStep{
		Name:                        fmt.Sprintf("%s maintenance", settingsName),
		ConsulSettingsName:          settingsName,
		CastServiceRegistrationFunc: castFunc,
		SkipHasChangesCheck:         true,
		AdditionalConditionFunc: func(ctx core.ExecutionContext, client consul.ConsulClient, registration *types.AgentServiceRegistration, logger *zap.Logger) (bool, error) {
			if !registration.Enabled {
				logger.Debug(fmt.Sprintf("Skipping maintenance since service %s registration is disabled", registration.ID))
				return false, nil
			}
			return true, nil
		},
		ExecuteFunc: func(ctx core.ExecutionContext, client consul.ConsulClient, srEnabled bool, proxyChecksHosts bool, serviceRegistration *consulApi.AgentServiceRegistration, logger *zap.Logger) error {
			return client.Maintenance(serviceRegistration.ID, isMainteaning, reason...)
		},
	}
}

func (r *ConsulSettingsWrapperStep) CastAndUpdateConsulServiceRegistration(ctx core.ExecutionContext, client consul.ConsulClient, registration *types.AgentServiceRegistration, logger *zap.Logger) *consulApi.AgentServiceRegistration {
	if r.CastServiceRegistrationFunc != nil {
		return r.CastServiceRegistrationFunc(ctx, client, registration, logger)
	} else {
		return &registration.AgentServiceRegistration
	}
}

func FindServiceRegistrationByKey(ctx core.ExecutionContext, key string) *types.AgentServiceRegistration {
	regs := ctx.Get(constants.ContextConsulServiceRegistrations).(map[string]*types.AgentServiceRegistration)
	return regs[key]
}
