package env

import (
	"context"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
)

// Runtime is the sandbox driver. Docker and Kubernetes both implement this.
type Runtime interface {
	Name() string
	Create(ctx context.Context, spec docker.Spec) (*docker.Handle, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Destroy(ctx context.Context, id string) error
	Recreate(ctx context.Context, spec docker.Spec) (*docker.Handle, error)
	Get(ctx context.Context, id string) (*docker.Handle, error)
	List(ctx context.Context) ([]*docker.Handle, error)
	Endpoints(ctx context.Context, id string) (map[int]string, error)
	Logs(ctx context.Context, id string, tail int) (string, error)
	ImageExists(ctx context.Context, ref string) (bool, error)
	ImagePull(ctx context.Context, ref string) error
	ImageTag(ctx context.Context, src, dst string) error
}

// DestroyAtSyncer optionally persists DestroyAt onto the runtime workload
// (e.g. a Kubernetes Deployment annotation) so idle sweep can see renewals
// across control-plane processes.
type DestroyAtSyncer interface {
	SetDestroyAt(ctx context.Context, id string, at time.Time) error
	GetDestroyAt(ctx context.Context, id string) (*time.Time, error)
}
