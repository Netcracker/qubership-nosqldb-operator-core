package core

type Executable interface {
	Validate(ctx ExecutionContext) error
	Execute(ctx ExecutionContext) error
	Condition(ctx ExecutionContext) (bool, error)
}

type DefaultExecutable struct {
	Executable
}

func (r *DefaultExecutable) Validate(ctx ExecutionContext) error {
	return nil
}

func (r *DefaultExecutable) Condition(ctx ExecutionContext) (bool, error) {
	return true, nil
}
