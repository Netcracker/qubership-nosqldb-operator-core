package fake

import (
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/core"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FakeServicesCompound struct {
	core.DefaultCompound
}

type FakeServiceBuilder struct {
	core.ExecutableBuilder
}

func (r *FakeServiceBuilder) Build(ctx core.ExecutionContext) core.Executable {
	//spec := ctx.Get(constants.ContextSpec).(*FakeService)
	client := ctx.Get(constants.ContextClient).(client.Client)
	log := ctx.Get(constants.ContextLogger).(*zap.Logger)
	log.Debug("Fake Executable build process is started")

	defaultUtilsHelper := &core.DefaultKubernetesHelperImpl{
		ForceKey: true,
		OwnerKey: false,
		Client:   client,
	}
	ctx.Set("utilsHelperImpl", defaultUtilsHelper)

	var compound core.ExecutableCompound = &FakeServicesCompound{}
	compound.AddStep((&FakeBuilder{}).Build(ctx))

	log.Debug("Fake Executable has been built")

	return compound
}
