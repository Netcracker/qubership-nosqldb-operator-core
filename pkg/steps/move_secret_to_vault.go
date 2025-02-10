package steps

import (
	"fmt"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/core"
	mTypes "github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/vault"
	"go.uber.org/zap"
)

type MoveSecretToVault struct {
	core.DefaultExecutable
	Password              string
	SecretName            string
	PolicyName            string
	Policy                string
	VaultRegistration     *mTypes.VaultRegistration
	CtxVarToStorePassword string
	ConditionFunc         func() (bool, error)
}

func (r *MoveSecretToVault) Execute(ctx core.ExecutionContext) error {
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	vaultHelper := ctx.Get(constants.ContextVault).(vault.VaultHelper)
	log.Info("vault.MoveSecretToVault step is started")

	var err error

	if r.Password == "" {
		r.Password, err = vaultHelper.GeneratePassword(r.Policy)
	}
	core.PanicError(err, log.Error, fmt.Sprintf("Failed to generate password for secret %s", r.SecretName))

	err = vaultHelper.StorePassword(r.SecretName, r.Password)
	core.PanicError(err, log.Error, fmt.Sprintf("Failed to store password for secret %s", r.SecretName))

	if r.CtxVarToStorePassword != "" {
		ctx.Set(r.CtxVarToStorePassword, r.Password)
	}
	log.Info("vault.MoveSecretToVault step is ended")
	return nil
}

func (r *MoveSecretToVault) Condition(ctx core.ExecutionContext) (bool, error) {
	if r.ConditionFunc != nil {
		return r.ConditionFunc()
	}
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	v := ctx.Get(constants.ContextVault).(vault.VaultHelper)

	passwordExists, password, secretExistsErr := checkPasswordExists(v, r.SecretName)
	core.PanicError(secretExistsErr, log.Error, fmt.Sprintf("Reading secret %s in vault failed", r.SecretName))

	if passwordExists {
		log.Info("Secret already present in Vault, skipping step")
		if r.CtxVarToStorePassword != "" {
			ctx.Set(r.CtxVarToStorePassword, password)
		}
	}
	//Condition is reversed - if password exists skipping step
	return !passwordExists, nil
}

func checkPasswordExists(vaultHelper vault.VaultHelper, secretName string) (bool, string, error) {
	secretExists, secret, err := vaultHelper.CheckSecretExists(secretName)

	if err != nil || !secretExists {
		return false, "", err
	} else {
		return secretExists, secret[constants.Password].(string), err
	}
}
