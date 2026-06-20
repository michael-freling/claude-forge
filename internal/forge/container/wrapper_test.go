package container

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewClient verifies the real constructor builds a Client backed by the
// docker SDK wrapper. Construction is lazy and does not dial the daemon.
func TestNewClient(t *testing.T) {
	c, err := NewClient()
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.IsType(t, &dockerAPIWrapper{}, c.docker)
	require.NoError(t, c.Close())
}

// TestDockerAPIWrapper_Delegates exercises every dockerAPIWrapper method to
// confirm it forwards to the underlying SDK client. The client points at a
// refused address, so each call returns a connection error — what matters is
// that the delegation path executes.
func TestDockerAPIWrapper_Delegates(t *testing.T) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.WithHost("tcp://127.0.0.1:1"))
	require.NoError(t, err)
	w := &dockerAPIWrapper{client: cli}
	ctx := context.Background()

	_, err = w.ContainerCreate(ctx, &container.Config{}, &container.HostConfig{}, &network.NetworkingConfig{}, "name")
	assert.Error(t, err)
	assert.Error(t, w.ContainerStart(ctx, "id", container.StartOptions{}))
	assert.Error(t, w.ContainerStop(ctx, "id", container.StopOptions{}))
	assert.Error(t, w.ContainerRemove(ctx, "id", container.RemoveOptions{}))
	_, err = w.ContainerList(ctx, container.ListOptions{})
	assert.Error(t, err)
	_, err = w.ContainerInspect(ctx, "id")
	assert.Error(t, err)
	_, err = w.ContainerLogs(ctx, "id", container.LogsOptions{})
	assert.Error(t, err)
	_, err = w.NetworkCreate(ctx, "net", network.CreateOptions{})
	assert.Error(t, err)
	assert.Error(t, w.NetworkRemove(ctx, "net"))
	assert.Error(t, w.NetworkConnect(ctx, "net", "id", &network.EndpointSettings{}))
	_, err = w.NetworkList(ctx, network.ListOptions{})
	assert.Error(t, err)
	_, err = w.ImagePull(ctx, "ref", image.PullOptions{})
	assert.Error(t, err)
	_, err = w.ImageList(ctx, image.ListOptions{})
	assert.Error(t, err)
	assert.NoError(t, w.Close())
}
