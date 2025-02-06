package steps

import (
	"fmt"
	"time"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	mTypes "github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/vault"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
)

type createDBEngine struct {
	core.DefaultExecutable
	ConfigName     string
	RoleName       string
	ConfigSettings map[string]interface{}
	RoleSettings   map[string]interface{}
	RolePath       string
	WaitTimeout    int
	ConditionFunc  func() (bool, error)
}

func NewCreateDBEngine(configName string, configSettings map[string]interface{}, roleName string, rolePath string, roleSettings map[string]interface{}) *createDBEngine {
	return &createDBEngine{
		ConfigName:     configName,
		RoleName:       roleName,
		ConfigSettings: configSettings,
		RoleSettings:   roleSettings,
		RolePath:       rolePath,
	}
}

func (r *createDBEngine) Execute(ctx core.ExecutionContext) error {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	v := ctx.Get(constants.ContextVault).(vault.VaultHelper)
	//in case if some of variables are lazy
	parseSettings(r.ConfigSettings)

	var err error
	dbError := wait.PollImmediate(5*time.Second, time.Second*time.Duration(120), func() (bool, error) {
		log.Debug(fmt.Sprintf("Trying to configurate Database %s", r.ConfigName))
		vaultErr := v.CreateDatabaseConfig(r.ConfigName, r.ConfigSettings)
		if vaultErr != nil {
			log.Debug(fmt.Sprintf("Failed to configurate DB engine with err %v", vaultErr))
			err = vaultErr
		}

		return vaultErr == nil, nil
	})
	core.PanicError(dbError, log.Error, fmt.Sprintf("All attempts to configurate DB engine %s failed. Response error is %v", r.ConfigName, err))

	parseSettings(r.RoleSettings)
	err = v.CreateStaticRole(r.RolePath+r.RoleName, r.RoleSettings)
	core.PanicError(err, log.Error, fmt.Sprintf("Could not create role for DB engine %s", r.RoleName))
	return nil
}

func parseSettings(settings map[string]interface{}) {
	for key, value := range settings {
		if f, ok := value.(func() string); ok {
			settings[key] = f()
		}
	}
}

type SetPasswordFromVaultRole struct {
	core.DefaultExecutable
	Registration          mTypes.VaultRegistration
	RoleName              string
	CtxVarToStorePassword string
}

func (r *SetPasswordFromVaultRole) Execute(ctx core.ExecutionContext) error {
	vaultHelper := ctx.Get(constants.ContextVault).(vault.VaultHelper)
	roleMap, err := vaultHelper.GetStaticRoleCredentials(r.RoleName)
	ctx.Set(r.CtxVarToStorePassword, roleMap["password"])
	return err
}

func (r *SetPasswordFromVaultRole) Condition(ctx core.ExecutionContext) (bool, error) {
	vaultHelper := ctx.Get(constants.ContextVault).(vault.VaultHelper)
	exists, err := vaultHelper.IsStaticRoleExists(r.RoleName)
	if err != nil {
		return false, nil
	}
	return exists, nil
}
