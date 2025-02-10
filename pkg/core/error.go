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

type NotFoundError struct {
	Msg string
}

func (r *NotFoundError) Error() string {
	return r.Msg
}

func NewNotFoundError(msg string) *NotFoundError {
	return &NotFoundError{Msg: msg}
}
