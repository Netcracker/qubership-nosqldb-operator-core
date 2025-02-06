package core

type Executor struct {
	executable        *interface{}
	executionStrategy func(executable *interface{}, ctx ExecutionContext) error
}

func (r *Executor) SetExecutable(executable Executable) {
	var ex interface{} = executable
	r.executable = &ex
}

func (r *Executor) Execute(ctx ExecutionContext) error {
	return r.executionStrategy(r.executable, ctx)
}

func DefaultExecutor() Executor {
	strategy := func(executable *interface{}, ctx ExecutionContext) error {
		steps := []func() error{
			func() error { return ((*executable).(Executable)).Validate(ctx) },
			func() error { return ((*executable).(Executable)).Execute(ctx) },
		}
		for _, element := range steps {
			err := element()
			if err != nil {
				return err
			}
		}
		return nil
	}
	return Executor{executionStrategy: strategy}
}
