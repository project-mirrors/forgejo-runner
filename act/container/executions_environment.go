package container

import "context"

type ExecutionsEnvironment interface {
	Container
	ToContainerPath(string) string
	GetName() string
	GetRoot() string
	GetLXC() bool
	GetActPath() string
	GetPathVariableName() string
	DefaultPathVariable() string
	JoinPathVariable(...string) string
	GetRunnerContext(ctx context.Context) map[string]any
	// On windows PATH and Path are the same key
	IsEnvironmentCaseInsensitive() bool
}
