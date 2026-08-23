package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerClient abstracts Docker API operations for testing.
type DockerClient interface {
	// Container operations
	ContainerCreate(ctx context.Context, config *container.Config,
		hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig,
		platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerPause(ctx context.Context, containerID string) error
	ContainerUnpause(ctx context.Context, containerID string) error

	// Image operations
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)

	// Network operations
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)

	// NetworkConnect attaches a running container to a network. Restricted
	// runners are created detached and connected only once their firewall
	// rules exist, so this is the moment egress becomes possible at all.
	NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
	NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error

	// Connection
	Ping(ctx context.Context) (types.Ping, error)
	Close() error
}

// dockerClientWrapper wraps the official Docker client to implement DockerClient.
type dockerClientWrapper struct {
	*client.Client
}

// NewDockerClient creates a Docker client from config.
func NewDockerClient(cfg *Config) (DockerClient, error) {
	opts := []client.Opt{
		client.WithHost(cfg.Host),
		client.WithAPIVersionNegotiation(),
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}

	return &dockerClientWrapper{Client: cli}, nil
}

// Ensure dockerClientWrapper implements DockerClient.
var _ DockerClient = (*dockerClientWrapper)(nil)
