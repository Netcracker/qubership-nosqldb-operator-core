package vault

import (
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
)

type FakeVaultHelper struct {
	mock.Mock
}

func (v FakeVaultHelper) CreateDatabaseConfig(configName string, configSettings map[string]interface{}) error {
	args := v.Called(configName, configSettings)
	return args.Error(0)
}

func (v FakeVaultHelper) IsDatabaseConfigExist(configName string) (bool, error) {
	args := v.Called(configName)
	return args.Bool(0), args.Error(1)
}

func (v FakeVaultHelper) CreateStaticRole(rolePath string, roleSettings map[string]interface{}) error {
	args := v.Called(rolePath, roleSettings)
	return args.Error(0)
}

func (v FakeVaultHelper) IsStaticRoleExists(rolePath string) (bool, error) {
	args := v.Called(rolePath)
	return args.Bool(0), args.Error(1)
}

func (v FakeVaultHelper) GetStaticRoleCredentials(roleName string) (map[string]interface{}, error) {
	args := v.Called(roleName)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (v FakeVaultHelper) GeneratePassword(policy string) (string, error) {
	args := v.Called(policy)
	return args.String(0), args.Error(1)
}
func (v FakeVaultHelper) StorePassword(secretName string, password string) error {
	args := v.Called(secretName, password)
	return args.Error(0)
}

func (v FakeVaultHelper) CheckSecretExists(secretName string) (bool, map[string]interface{}, error) {
	args := v.Called(secretName)
	return args.Bool(0), args.Get(1).(map[string]interface{}), args.Error(2)
}

func (v FakeVaultHelper) RotateRole(roleName string) error {
	args := v.Called(roleName)
	return args.Error(0)
}

func (v FakeVaultHelper) GetEnvTemplateForVault(envName string, secretName string) v1.EnvVar {
	args := v.Called(envName, secretName)
	return args.Get(0).(v1.EnvVar)
}

func (v FakeVaultHelper) IsVaultURL(path string) bool {
	args := v.Called(path)
	return args.Bool(0)
}
