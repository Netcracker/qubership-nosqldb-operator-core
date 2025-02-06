package core

type ExecutableBuilder interface {
	Build(ctx ExecutionContext) Executable
}
