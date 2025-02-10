package vault

import (
	"fmt"
	"io/ioutil"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	"github.com/docker/distribution/uuid"
	"github.com/hashicorp/vault/api"
)

type VaultClientImpl struct {
	VaultRegistration *types.VaultRegistration
	client            *api.Client
}

func NewVaultClientImpl(vaultRegistration *types.VaultRegistration) VaultClientImpl {
	return VaultClientImpl{VaultRegistration: vaultRegistration}
}

func (r VaultClientImpl) GetToken() (string, error) {
	jwtToken, err := ReadFromFile(constants.TokenFilePath)
	options := map[string]interface{}{
		"jwt":  jwtToken,
		"role": r.VaultRegistration.Role,
	}
	config := &api.Config{
		Address: r.VaultRegistration.Url,
	}
	loginPath := "/auth/" + r.VaultRegistration.Method + "/login"
	client, err := api.NewClient(config)
	if err != nil {
		return "", err
	}
	clientToken, err := client.Logical().Write(loginPath, options)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s", clientToken.Auth.ClientToken), nil
}

func (r VaultClientImpl) GetClient() *api.Client {
	config := &api.Config{
		Address: r.VaultRegistration.Url,
	}

	client, err := api.NewClient(config)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	return client
}

func (r *VaultClientImpl) refreshClient() error {
	clientToken, err := r.GetToken()
	if err != nil {
		return err
	}
	r.client = r.GetClient()
	r.client.SetToken(clientToken)
	return nil
}

func (r VaultClientImpl) VaultRead(path string) (map[string]interface{}, error) {
	r.refreshClient()
	secret, err := r.client.Logical().Read(path)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (r VaultClientImpl) VaultWrite(path string, secret map[string]interface{}) error {
	err := r.refreshClient()
	if err != nil {
		return err
	}
	_, err = r.client.Logical().Write(path, secret)
	if err != nil {
		return err
	}
	return nil
}

func (r VaultClientImpl) VaultList(path string) (*api.Secret, error) {
	err := r.refreshClient()
	if err != nil {
		return nil, err
	}
	secret, err := r.client.Logical().List(path)
	if err != nil {
		return nil, err
	}
	return secret, err
}

// not supported yet
func (r VaultClientImpl) VaultGeneratePasswordWithPolicy(policyName string) (string, error) {
	//request := client.NewRequest("GET", fmt.Sprintf("/sys/policies/password/%s/generate", policyName))
	//
	//resp, respErr := client.RawRequest(request)
	//if respErr != nil {
	//	return "", respErr
	//}
	//password := make(map[string]string)
	//if resp.StatusCode == 200 {
	//	decodeErr := resp.DecodeJSON(password)
	//	if decodeErr != nil {
	//		return "", decodeErr
	//	}
	//}
	//return password["password"], nil

	return uuid.Generate().String(), nil
}

func (r VaultClientImpl) VaultCreatePasswordPolicy(policyName string, policy string) error {
	r.refreshClient()
	request := r.client.NewRequest("PUT", "/sys/policies/password/"+policyName)
	err := request.SetJSONBody(map[string]string{"policy": policy})
	if err != nil {
		return err
	}
	resp, respErr := r.client.RawRequest(request)
	if respErr != nil {
		return respErr
	}
	if resp.StatusCode != 200 {
		return nil //todo
	}
	return nil
}

func ReadFromFile(filePath string) (string, error) {
	dat, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}
