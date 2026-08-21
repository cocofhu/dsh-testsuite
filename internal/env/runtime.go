package env

import (
	"context"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
)

// Runtime is the sandbox driver. The docker implementation is the only one
// in this round; kubernetes can satisfy the same surface later.
type Runtime interface {
	Create(ctx context.Context, spec docker.Spec) (*docker.Handle, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Destroy(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*docker.Handle, error)
	List(ctx context.Context) ([]*docker.Handle, error)
	Endpoints(ctx context.Context, id string) (map[int]string, error)
	Logs(ctx context.Context, id string, tail int) (string, error)
	ImageExists(ctx context.Context, ref string) (bool, error)
	ImagePull(ctx context.Context, ref string) error
	ImageTag(ctx context.Context, src, dst string) error
}
