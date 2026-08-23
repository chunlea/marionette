//go:build integration

package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

// Live network-isolation tests against a real Docker daemon.
//
// These need more than a Docker socket, which is why they are not part of the
// normal integration run:
//
//   - Linux, running as root. Rules are written with iptables inside another
//     process's network namespace, which needs CAP_NET_ADMIN there.
//   - Visibility of the runner containers' PIDs. The Docker API reports a PID
//     in the daemon's PID namespace; /proc/<pid>/ns/net only resolves if the
//     test process shares it.
//   - nsenter, iptables and bash on PATH.
//
// From a macOS or Windows host, the repo's Linux test image satisfies all of
// them:
//
//	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
//	docker run --rm --privileged --pid=host \
//	  -v /var/run/docker.sock:/var/run/docker.sock \
//	  -e DOCKER_HOST=unix:///var/run/docker.sock \
//	  marionette/test:latest \
//	  go test -tags=integration -v -run TestIsolation ./pkg/provider/docker/
//
// --pid=host is the part that matters: without it the container PIDs the
// daemon reports do not exist in this process's namespace and every test here
// skips. --privileged supplies CAP_NET_ADMIN and CAP_SYS_ADMIN for nsenter.
//
// Anything missing makes the test skip rather than fail: a false green on a
// machine that cannot run it would be worse than no test.

const (
	isolationTestImage = "alpine:3.20"
	isolationNetwork   = "marionette-isolation-test"
)

// requireIsolationEnv skips unless this machine can enforce and observe rules.
func requireIsolationEnv(t *testing.T) DockerClient {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("network namespace tests need Linux; see the file comment for the container invocation")
	}
	if os.Geteuid() != 0 {
		t.Skip("writing iptables in another namespace needs root")
	}
	for _, bin := range []string{"nsenter", "iptables", "bash"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not on PATH", bin)
		}
	}

	client, err := NewDockerClient(&Config{Host: dockerHostFromEnv()})
	if err != nil {
		t.Skipf("no Docker client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx); err != nil {
		t.Skipf("Docker daemon unreachable: %v", err)
	}

	return client
}

func dockerHostFromEnv() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host
	}
	return DefaultHost
}

// pullTestImage makes sure the probe image is present.
func pullTestImage(t *testing.T, client DockerClient) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	rc, err := client.ImagePull(ctx, isolationTestImage, image.PullOptions{})
	if err != nil {
		t.Skipf("cannot pull %s: %v", isolationTestImage, err)
	}
	defer rc.Close()

	// The pull only completes once the body is drained.
	buf := make([]byte, 4096)
	for {
		if _, err := rc.Read(buf); err != nil {
			break
		}
	}
}

// nsenterOutput runs a command inside a container's network namespace.
func nsenterOutput(t *testing.T, pid int, args ...string) (string, error) {
	t.Helper()

	full := append([]string{fmt.Sprintf("--net=/proc/%d/ns/net", pid), "--"}, args...)
	cmd := exec.Command("nsenter", full...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// canReach reports whether a TCP connection from inside the namespace lands.
//
// bash's /dev/tcp avoids depending on a netcat build; timeout bounds the wait
// because a DROP produces no response at all.
func canReach(t *testing.T, pid int, addr string) bool {
	t.Helper()

	target := strings.Replace(addr, ":", "/", 1)
	_, err := nsenterOutput(t, pid, "timeout", "4", "bash", "-c",
		fmt.Sprintf("exec 3<>/dev/tcp/%s", target))
	return err == nil
}

// observingClient reports what the container looked like at the instant the
// provider connected it to a network.
type observingClient struct {
	DockerClient

	t              *testing.T
	linksAtConnect string
	rulesAtConnect string
	observeErr     error
}

func (c *observingClient) NetworkConnect(ctx context.Context, networkID, containerID string, cfg *network.EndpointSettings) error {
	info, err := c.DockerClient.ContainerInspect(ctx, containerID)
	if err != nil || info.State == nil || info.State.Pid == 0 {
		c.observeErr = fmt.Errorf("inspecting container before connect: %w", err)
		return c.DockerClient.NetworkConnect(ctx, networkID, containerID, cfg)
	}

	c.linksAtConnect, _ = nsenterOutput(c.t, info.State.Pid, "ip", "-o", "link", "show")
	c.rulesAtConnect, _ = nsenterOutput(c.t, info.State.Pid, "iptables", "-S")

	return c.DockerClient.NetworkConnect(ctx, networkID, containerID, cfg)
}

// startTarget runs a container listening on 8080 and returns its address.
func startTarget(t *testing.T, client DockerClient, networkName string) (string, string) {
	t.Helper()

	ctx := context.Background()
	resp, err := client.ContainerCreate(ctx,
		&container.Config{
			Image: isolationTestImage,
			// A listener that accepts and immediately closes is enough: the
			// probe only needs the TCP handshake to complete.
			Cmd: []string{"sh", "-c", "while true; do nc -l -p 8080 -e true || true; done"},
		},
		&container.HostConfig{},
		&network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}},
		},
		(*ocispec.Platform)(nil), "")
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, client.ContainerStart(ctx, resp.ID, container.StartOptions{}))

	// Give the listener a moment to bind.
	time.Sleep(500 * time.Millisecond)

	info, err := client.ContainerInspect(ctx, resp.ID)
	require.NoError(t, err)

	settings, ok := info.NetworkSettings.Networks[networkName]
	require.True(t, ok, "target container is not on %s", networkName)
	require.NotEmpty(t, settings.IPAddress)

	return resp.ID, settings.IPAddress
}

// isolationProvider builds a provider wired to the observing client.
func isolationProvider(t *testing.T, client DockerClient, isolation IsolationConfig) (*Provider, *observingClient) {
	t.Helper()

	observer := &observingClient{DockerClient: client, t: t}
	cfg := &Config{
		Host:      dockerHostFromEnv(),
		Image:     isolationTestImage,
		Network:   isolationNetwork,
		Cmd:       []string{"sleep", "300"},
		Isolation: isolation,
	}
	cfg.applyDefaults()
	require.NoError(t, cfg.validate())

	p := NewWithClient("docker-isolation-test", cfg, nil, observer)
	t.Cleanup(p.networkIsolation.StopAll)

	return p, observer
}

// containerPID returns the PID of a spawned runner's container.
func containerPID(t *testing.T, client DockerClient, containerID string) int {
	t.Helper()

	info, err := client.ContainerInspect(context.Background(), containerID)
	require.NoError(t, err)
	require.NotNil(t, info.State)
	require.NotZero(t, info.State.Pid, "container is not running")
	return info.State.Pid
}

// TestIsolationAirGapped_NoInterfaceExistsUntilRulesAreInstalled is the live
// proof for the startup window.
//
// The unit test asserts the ordering of the calls; this asserts the state of
// the kernel at the moment that ordering matters. When the provider is about
// to attach a network:
//
//   - the container's namespace holds only loopback, so no packet the process
//     inside emits has anywhere to go, and
//   - the iptables chain is already complete, ending in a default DROP.
//
// It then verifies the policy actually bites once the interface appears: the
// pinned control plane is reachable and a neighbour on the same subnet is not.
func TestIsolationAirGapped_NoInterfaceExistsUntilRulesAreInstalled(t *testing.T) {
	client := requireIsolationEnv(t)
	defer client.Close()

	pullTestImage(t, client)

	// A neighbour to serve as the control plane, and another as the address
	// that must stay unreachable.
	p, observer := isolationProvider(t, client, IsolationConfig{})
	require.NoError(t, p.ensureNetwork(context.Background()))

	_, allowedIP := startTarget(t, client, isolationNetwork)
	_, deniedIP := startTarget(t, client, isolationNetwork)
	require.NotEqual(t, allowedIP, deniedIP)

	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_isolation_airgap",
		Name:          "isolation-airgap",
		ServerURL:     allowedIP + ":8080",
		NetworkPolicy: "air_gapped",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Destroy(context.Background(), instance.ID) })

	require.NoError(t, observer.observeErr)

	// 1. Only loopback existed when the network was attached. Every other
	//    interface in a container's namespace is created by that attach, so
	//    there was no egress path of any kind before this point.
	require.NotEmpty(t, observer.linksAtConnect, "failed to read interfaces before connect")
	assert.Contains(t, observer.linksAtConnect, "lo:")
	assert.NotContains(t, observer.linksAtConnect, "eth0",
		"the container had an interface before its rules were installed:\n%s", observer.linksAtConnect)

	// 2. The rules were already complete at that instant.
	assert.Contains(t, observer.rulesAtConnect, "-A OUTPUT -j MARIONETTE_run_isolation")
	assert.Contains(t, observer.rulesAtConnect, "-j DROP")
	assert.Contains(t, observer.rulesAtConnect, allowedIP,
		"the control plane pin is missing:\n%s", observer.rulesAtConnect)

	// 3. And the policy holds now that the interface exists.
	pid := containerPID(t, client, instance.Metadata["container_id"])

	links, err := nsenterOutput(t, pid, "ip", "-o", "link", "show")
	require.NoError(t, err)
	assert.Contains(t, links, "eth0", "the container should be connected by now")

	assert.True(t, canReach(t, pid, allowedIP+":8080"),
		"the pinned control plane must stay reachable; it sits inside a blocked private range and the pin has to win")
	assert.False(t, canReach(t, pid, deniedIP+":8080"),
		"air-gapped must not reach a neighbour on its own subnet")
}

// TestIsolationAllowList_OpensOnlyTheResolvedAddresses checks the allow list
// against live traffic.
func TestIsolationAllowList_OpensOnlyTheResolvedAddresses(t *testing.T) {
	client := requireIsolationEnv(t)
	defer client.Close()

	pullTestImage(t, client)

	p, _ := isolationProvider(t, client, IsolationConfig{})
	require.NoError(t, p.ensureNetwork(context.Background()))

	_, controlIP := startTarget(t, client, isolationNetwork)
	_, deniedIP := startTarget(t, client, isolationNetwork)

	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_isolation_allowlist",
		Name:          "isolation-allowlist",
		ServerURL:     controlIP + ":8080",
		NetworkPolicy: "allow_list",
		// A neighbour's address would be dropped by the private-range block
		// even if it were allowed, which is the point: the block wins over the
		// allow list, and only an operator pin beats a block.
		AllowedHosts: []string{"example.com"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Destroy(context.Background(), instance.ID) })

	pid := containerPID(t, client, instance.Metadata["container_id"])

	assert.True(t, canReach(t, pid, controlIP+":8080"), "the control plane must stay reachable")
	assert.False(t, canReach(t, pid, deniedIP+":8080"), "a private neighbour must stay blocked")

	// The metadata endpoint is blocked at every level and is not configurable.
	assert.False(t, canReach(t, pid, "169.254.169.254:80"), "the cloud metadata endpoint must never be reachable")
}

// TestIsolationNone_LeavesTheContainerAlone guards the unrestricted path: the
// smoke runner and every default session use it, and none of the isolation
// machinery may touch them.
func TestIsolationNone_LeavesTheContainerAlone(t *testing.T) {
	client := requireIsolationEnv(t)
	defer client.Close()

	pullTestImage(t, client)

	p, observer := isolationProvider(t, client, IsolationConfig{})
	require.NoError(t, p.ensureNetwork(context.Background()))

	_, targetIP := startTarget(t, client, isolationNetwork)

	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_isolation_none",
		Name:          "isolation-none",
		NetworkPolicy: "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Destroy(context.Background(), instance.ID) })

	// Attached at creation, so the provider never called NetworkConnect.
	assert.Empty(t, observer.linksAtConnect)

	pid := containerPID(t, client, instance.Metadata["container_id"])

	rules, err := nsenterOutput(t, pid, "iptables", "-S")
	require.NoError(t, err)
	assert.NotContains(t, rules, "MARIONETTE_", "an unrestricted runner must have no rules at all")

	assert.True(t, canReach(t, pid, targetIP+":8080"), "an unrestricted runner reaches its neighbours")
}
