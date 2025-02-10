package vault

import (
	"fmt"
	"strings"

	constants "github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/utils"
	v1 "k8s.io/api/core/v1"
)

type VaultHelper interface {
	CreateDatabaseConfig(configName string, configSettings map[string]interface{}) error
	IsDatabaseConfigExist(configName string) (bool, error)
	CreateStaticRole(rolePath string, roleSettings map[string]interface{}) error
	GetStaticRoleCredentials(roleName string) (map[string]interface{}, error)
	IsStaticRoleExists(rolePath string) (bool, error)
	GeneratePassword(policy string) (string, error)
	StorePassword(secretName string, password string) error
	CheckSecretExists(secretName string) (bool, map[string]interface{}, error)
	RotateRole(roleName string) error
	IsVaultURL(path string) bool
	GetEnvTemplateForVault(envName string, secretName string) v1.EnvVar
	ResolvePassword(passAddress string) (string, error)
}

type VaulterHelperImpl struct {
	VaultClient VaultClientImpl
}

func NewVaulterHelperImpl(vc VaultClientImpl) VaultHelper {
	return VaulterHelperImpl{
		VaultClient: vc,
	}
}

func (v VaulterHelperImpl) ResolvePassword(passAddress string) (string, error) {
	firstDelimiter := strings.Index(passAddress, ":")
	secondDelimiter := strings.Index(passAddress, "#")
	if firstDelimiter < 0 || secondDelimiter < 0 {
		return "", fmt.Errorf("provided passAddress does not contain : or # delimiter")
	}
	role, err := v.VaultClient.VaultRead(passAddress[firstDelimiter+1 : secondDelimiter])
	if err != nil {
		return "", err
	}
	return role["password"].(string), nil
}

func (v VaulterHelperImpl) CreateDatabaseConfig(configName string, configSettings map[string]interface{}) error {
	return v.VaultClient.VaultWrite("/database/config/"+configName, configSettings)
}

func (v VaulterHelperImpl) IsDatabaseConfigExist(configName string) (bool, error) {
	secret, err := v.VaultClient.VaultRead("/database/config/" + configName)
	if err != nil {
		return false, err
	} else if secret != nil && len(secret) > 0 {
		return true, nil
	} else {
		return false, nil
	}
}

func (v VaulterHelperImpl) IsStaticRoleExists(rolePath string) (bool, error) {
	secret, err := v.VaultClient.VaultList("database/static-roles")
	if secret != nil {
		for _, v := range secret.Data["keys"].([]interface{}) {
			if v.(string) == rolePath {
				return true, nil
			}
		}
	}
	return false, err
}

func (v VaulterHelperImpl) CreateStaticRole(rolePath string, roleSettings map[string]interface{}) error {
	return v.VaultClient.VaultWrite(rolePath, roleSettings)
}

func (v VaulterHelperImpl) GetStaticRoleCredentials(roleName string) (map[string]interface{}, error) {
	return v.VaultClient.VaultRead(fmt.Sprintf("database/static-creds/%s", roleName))
}

func (v VaulterHelperImpl) GeneratePassword(policy string) (string, error) {
	password, err := v.VaultClient.VaultGeneratePasswordWithPolicy(policy)
	if err != nil {
		return "", err
	}
	return password, nil
}
func (v VaulterHelperImpl) StorePassword(secretName string, password string) error {
	vaultPath := v.VaultClient.VaultRegistration.Path + "/" + secretName

	secretToWrite := make(map[string]interface{})
	secretToWrite[constants.Password] = password

	err := v.VaultClient.VaultWrite(vaultPath, secretToWrite)
	if err != nil {
		return err
	}

	return nil
}

func (v VaulterHelperImpl) CheckSecretExists(secretName string) (bool, map[string]interface{}, error) {
	vaultPath := v.VaultClient.VaultRegistration.Path + "/" + secretName
	secret, err := v.VaultClient.VaultRead(vaultPath)

	if err != nil {
		return false, nil, err
	}

	return secret != nil && len(secret) > 0, secret, nil
}

// TODO envvar
func (v VaulterHelperImpl) GetEnvTemplateForVault(envName string, secretName string) v1.EnvVar {
	return utils.GetEnvTemplateForVault(envName, secretName, constants.Password, v.VaultClient.VaultRegistration.Path)
}

func (v VaulterHelperImpl) IsVaultURL(path string) bool {
	return strings.HasPrefix(path, "vault:")
}

func (v VaulterHelperImpl) RotateRole(roleName string) error {
	path := "database/rotate-role/" + roleName
	return v.VaultClient.VaultWrite(path, nil)
}

func GetVaultArgs(arguments []string) []string {
	args := []string{
		"sh",
	}
	for _, v := range arguments {
		args = append(args, v)
	}
	return args
}

func GetVaultRoleName(cloudPublicHost, namespace, serviceAccount, user string) string {
	return fmt.Sprintf("nc-%s_%s_%s_%s", cloudPublicHost, namespace, serviceAccount, user)
}
