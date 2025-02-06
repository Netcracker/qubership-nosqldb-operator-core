package core

type ExecutionError struct {
	Msg string
}

func (r *ExecutionError) Error() string {
	return r.Msg
}

type DRExecutionError struct {
	Msg string
}

func (r *DRExecutionError) Error() string {
	return r.Msg
}
