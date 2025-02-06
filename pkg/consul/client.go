package consul

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/types"
	consulApi "github.com/hashicorp/consul/api"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"
)

type ConsulClient interface {
	Register(serviceRegistration *consulApi.AgentServiceRegistration) error
	Maintenance(serviceID string, isMainteaning bool, reason ...string) error
	Deregister(serviceID string) error
	Logout() error
}

type ConsulClientImpl struct {
	nodeIP             string
	namespace          string
	consulRegistration *types.ConsulRegistration
	kubeConfig         *rest.Config
	logger             *zap.Logger
	consulClient       *consulApi.Client
	aclToken           string
}

func NewConsulClientImpl(nodeIP string, namespace string, registration *types.ConsulRegistration, kubeConfig *rest.Config, logger *zap.Logger) (ConsulClient, error) {
	if registration != nil && registration.Enabled {
		client := ConsulClientImpl{
			nodeIP:             nodeIP,
			namespace:          namespace,
			consulRegistration: registration,
			kubeConfig:         kubeConfig,
			logger:             logger,
		}
		consul, err := client.createConsulClient()
		if err != nil {
			return nil, err
		}
		client.consulClient = consul
		logger.Debug("Consul client is created")
		return &client, nil
	} else {
		logger.Debug("Consul registration is not enabled")
		return nil, nil
	}
}

func (c *ConsulClientImpl) Register(serviceRegistration *consulApi.AgentServiceRegistration) error {
	err := c.consulClient.Agent().ServiceRegisterOpts(serviceRegistration, consulApi.ServiceRegisterOpts{ReplaceExistingChecks: true})
	if err != nil {
		return err
	}
	c.logger.Debug(fmt.Sprintf("Service '%s' with  ID '%s' and address '%s' is registered in Consul", serviceRegistration.Name, serviceRegistration.ID, serviceRegistration.Address))
	return nil
}

func (c *ConsulClientImpl) Maintenance(serviceID string, isMainteaning bool, reason ...string) error {
	var err error

	if isMainteaning {
		err = c.consulClient.Agent().EnableServiceMaintenance(serviceID, strings.Join(reason, ". "))
	} else {
		err = c.consulClient.Agent().DisableServiceMaintenance(serviceID)
	}
	if err != nil {
		return err
	}
	c.logger.Debug(fmt.Sprintf("Service with ID '%s' maintenance state switched to '%v'", serviceID, isMainteaning))
	return nil
}

func (c *ConsulClientImpl) Deregister(serviceID string) error {
	if err := c.consulClient.Agent().ServiceDeregister(serviceID); err != nil {
		return err
	}
	c.logger.Debug(fmt.Sprintf("Service with ID '%s' has been deregistred", serviceID))
	return nil
}

func (c *ConsulClientImpl) Logout() error {
	var err error
	if c.aclToken != "" {
		_, err = c.consulClient.ACL().Logout(&consulApi.WriteOptions{Token: c.aclToken})
	}
	c.logger.Debug("Consul client is logged-out")
	return err
}

// Check Impl implements interface
var _ ConsulClient = &ConsulClientImpl{}

func (c *ConsulClientImpl) createConsulClient() (*consulApi.Client, error) {
	consulConfig := consulApi.DefaultConfig()

	consulHost := c.consulRegistration.Host
	if len(consulHost) == 0 {
		consulHost = c.nodeIP
	}
	if len(consulHost) == 0 {
		panic("Consul host not fond in the registration data and nodeIP is empty")
		//return nil, &core.ExecutionError{Msg: "Consul host not fond in the registration data and nodeIP is empty"}
	}
	consulPort := c.consulRegistration.Port
	if len(consulPort) == 0 {
		c.logger.Debug("Default consul port will be used")
		consulPort = "8500"
	}
	consulConfig.Address = consulHost + ":" + consulPort
	c.logger.Debug("Consul address: " + consulConfig.Address)

	var clientFunc func(consulConfig *consulApi.Config) (*consulApi.Client, error) = c.saClient
	if c.consulRegistration.AclEnabled {
		clientFunc = c.aclClient
	}

	client, clientErr := clientFunc(consulConfig)
	return client, clientErr
}

func (c *ConsulClientImpl) aclClient(consulConfig *consulApi.Config) (*consulApi.Client, error) {

	token, err := readFromFile(constants.TokenFilePath)
	if err != nil {
		return nil, err
	}

	loginParams := &consulApi.ACLLoginParams{
		AuthMethod:  c.consulRegistration.AuthMethod,
		BearerToken: token,
	}
	client, err := consulApi.NewClient(consulConfig)
	if err != nil {
		return client, err
	}

	aclToken, _, tokenErr := client.ACL().Login(loginParams, &consulApi.WriteOptions{})
	if tokenErr != nil {
		return nil, tokenErr
	}
	consulConfig.Token = aclToken.SecretID
	c.aclToken = aclToken.SecretID
	time.Sleep(1 * time.Second)

	return consulApi.NewClient(consulConfig)
}

func (c *ConsulClientImpl) saClient(consulConfig *consulApi.Config) (*consulApi.Client, error) {
	//consulConfig.Namespace = c.namespace

	token, err := readFromFile(constants.TokenFilePath)
	if err != nil {
		return nil, err
	}
	consulConfig.Token = token

	client, clientErr := consulApi.NewClient(consulConfig)

	return client, clientErr
}

func readFromFile(filePath string) (string, error) {
	// For developing purposes
	dat, err := ioutil.ReadFile(getEnv("TELEPRESENCE_ROOT", "") + filePath)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}

// Imports cycle
func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}
