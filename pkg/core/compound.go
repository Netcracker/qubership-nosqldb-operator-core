package core

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"go.uber.org/zap"
)

type ExecutableCompound interface {
	Executable
	AddStep(step Executable)
}

type DefaultCompound struct {
	ExecutableCompound
	executableSteps []Executable
}

func (r *DefaultCompound) AddStep(step Executable) {
	r.executableSteps = append(r.executableSteps, step)
}

func (r *DefaultCompound) iterateOverSteps(stepFunc func(step Executable) error) error {
	for _, element := range r.executableSteps {
		err := stepFunc(element)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DefaultCompound) Validate(ctx ExecutionContext) error {
	return r.iterateOverSteps(
		func(element Executable) error {
			return element.Validate(ctx)
		})
}

func (r *DefaultCompound) Execute(ctx ExecutionContext) error {
	return r.iterateOverSteps(
		func(element Executable) error {
			if run, err := element.Condition(ctx); run {
				logger := ctx.Get(constants.ContextLogger)
				if logger != nil {
					log := ctx.Get(constants.ContextLogger).(*zap.Logger)
					st := reflect.TypeOf(element)
					stepName := strings.Split(st.String(), ".")[1]
					log.Info(fmt.Sprintf("Step %s started", stepName))
					defer log.Info(fmt.Sprintf("Step %s finished", stepName))
				}
				return element.Execute(ctx)
			} else {
				return err
			}
		})
}

func (r *DefaultCompound) Condition(ctx ExecutionContext) (bool, error) {
	return true, nil
}

type MicroServiceDeployType string

const (
	CleanDeploy MicroServiceDeployType = "CleanDeploy"
	Update      MicroServiceDeployType = "Update"
	Empty       MicroServiceDeployType = ""
)

type MicroServiceCompound struct {
	DefaultCompound
	ServiceName    string
	CalcDeployType func(ctx ExecutionContext) (MicroServiceDeployType, error)
}

// TODO why the same code?
func (r *MicroServiceCompound) Validate(ctx ExecutionContext) (executionErrResult error) {
	//Setting up current service deploy context
	previousDeployType := GetCurrentDeployType(ctx)

	// Handling microservice steps exception
	defer func() {
		//result := constants.MicroServiceSuccessDeploymentResult

		//panicStackTrace := string(debug.Stack())
		var errMsg string
		//if err := recover(); err != nil {
		//	// Panic
		//	var stringErrorMsg string
		//	switch v := err.(type) {
		//	case string:
		//		stringErrorMsg = v
		//	case error:
		//		stringErrorMsg = v.Error()
		//	}
		//
		//	errMsg = stringErrorMsg + "\n" + panicStackTrace
		//} else
		if executionErrResult != nil {
			// Usual exception
			//errMsg = executionErrResult.Error() + "\n" + panicStackTrace
			errMsg = executionErrResult.Error()
		}
		if errMsg != "" {
			//result = errMsg
			executionErrResult = &ExecutionError{Msg: "Microservice validation exception: " + errMsg}
		}

		//AddServiceDeployResultToContext(ctx, r.ServiceName, result)

		//Setting previous type
		SetCurrentDeployType(ctx, previousDeployType)
	}()

	//deployTypeForService := GetMicroServiceDeployType(ctx, r.ServiceName)
	deployTypeForService, err := r.CalcDeployType(ctx)
	if err != nil {
		executionErrResult = err
		return
	}
	SetCurrentDeployType(ctx, deployTypeForService)

	executionErrResult = r.DefaultCompound.Validate(ctx)

	return
}

func (r *MicroServiceCompound) Execute(ctx ExecutionContext) (executionErrResult error) {
	//Setting up current service deploy context
	previousDeployType := GetCurrentDeployType(ctx)

	// Handling microservice steps exception
	defer func() {
		//result := constants.MicroServiceSuccessDeploymentResult

		var errMsg string
		if executionErrResult != nil {
			// Usual exception
			errMsg = executionErrResult.Error()
		}
		if errMsg != "" {
			//result = errMsg
			executionErrResult = &ExecutionError{Msg: "Microservice execution exception: " + errMsg}
		}

		//AddServiceDeployResultToContext(ctx, r.ServiceName, result)

		//Setting previous type
		SetCurrentDeployType(ctx, previousDeployType)
	}()

	//deployTypeForService := GetMicroServiceDeployType(ctx, r.ServiceName)
	deployTypeForService, err := r.CalcDeployType(ctx)
	if err != nil {
		executionErrResult = err
		return
	}

	SetCurrentDeployType(ctx, deployTypeForService)

	executionErrResult = r.DefaultCompound.Execute(ctx)

	return
}
