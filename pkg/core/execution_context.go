package core

import (
	"fmt"
)

type ExecutionContext interface {
	Set(str string, obj interface{})
	Get(str string) interface{}
}

type DefaultExecutionContext struct {
	ExecutionContext
	contextVars map[string]interface{}
}

func NewDefaultExecutionContext() *DefaultExecutionContext {
	return NewInitExecutionContext(make(map[string]interface{}))
}

func NewInitExecutionContext(initValues map[string]interface{}) *DefaultExecutionContext {
	return &DefaultExecutionContext{contextVars: initValues}
}

func (r *DefaultExecutionContext) Set(str string, obj interface{}) {
	r.contextVars[str] = obj
}

func (r *DefaultExecutionContext) Get(str string) interface{} {
	if r.contextVars[str] == nil {
		println(fmt.Sprintf("No variable %s in execution context", str)) //todo log?
	}
	return r.contextVars[str]
}

func (r *DefaultExecutionContext) IsAnyNil(str ...string) (bool, error) {
	for _, element := range str {
		res := r.contextVars[element]
		if res == nil {
			return true, &ExecutionError{Msg: "Element " + element + " not found"}
		}
	}
	return false, nil
}

func GetExecutionContext(initValues map[string]interface{}) ExecutionContext {
	return NewInitExecutionContext(initValues)
}
