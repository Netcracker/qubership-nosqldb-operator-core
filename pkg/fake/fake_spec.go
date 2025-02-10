package fake

import (
	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeSpec struct {
	VaultRegistration  types.VaultRegistration    `json:"vaultRegistration"`
	Storage            *types.StorageRequirements `json:"storage,omitempty"`
	Resources          *v1.ResourceRequirements   `json:"resources,omitempty"`
	Policies           *Policies                  `json:"policies,omitempty"`
	PodSecurityContext *v1.PodSecurityContext     `json:"securityContext,omitempty"`
}

type Policies struct {
	Tolerations []v1.Toleration `json:"tolerations,omitempty" common:"true"`
}

type FakeService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FakeSpec          `json:"spec,omitempty"`
	Status FakeServiceStatus `json:"status,omitempty"`
}

type FakeServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FakeService `json:"items"`
}

type FakeServiceStatus struct {
	Conditions []types.ServiceStatusCondition `json:"conditions,omitempty"`
}
