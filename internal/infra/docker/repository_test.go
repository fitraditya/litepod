package docker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/pkg/logger"
)

// These are integration tests against a real Docker daemon (the interface
// this package wraps isn't practically fakeable without reimplementing the
// Docker Engine API). They're skipped automatically if no daemon is
// reachable, e.g. in a sandboxed CI runner without Docker.
const testImage = "redis:7-alpine"

// pullTestImageOnce ensures testImage is present locally exactly once per
// test binary run, so these tests don't depend on it being pre-cached on
// whatever machine/CI runner they execute on.
var (
	pullTestImageOnce sync.Once
	pullTestImageErr  error
)

func testRepo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client init failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}

	pullTestImageOnce.Do(func() {
		pullCtx, pullCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer pullCancel()
		out, err := cli.ImagePull(pullCtx, testImage, image.PullOptions{})
		if err != nil {
			pullTestImageErr = fmt.Errorf("pull %s: %w", testImage, err)
			return
		}
		defer out.Close()
		if _, err := io.Copy(io.Discard, out); err != nil {
			pullTestImageErr = fmt.Errorf("pull %s: %w", testImage, err)
		}
	})
	if pullTestImageErr != nil {
		t.Fatalf("%v", pullTestImageErr)
	}

	return NewRepository(cli, logger.NewSilent()), context.Background()
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("agentbox-test-%s-%d", t.Name(), time.Now().UnixNano())
}

func runTestContainer(t *testing.T, r *Repository, ctx context.Context, name string) string {
	t.Helper()
	id, err := r.Run(ctx, domain.DeploySpec{
		Image:         testImage,
		Name:          name,
		MemoryLimit:   64 * 1024 * 1024,
		RestartPolicy: "no",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = r.Remove(context.Background(), name, true, true)
	})
	return id
}

func TestRepository_RunStateListRemove(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)

	id := runTestContainer(t, r, ctx, name)
	assert.NotEmpty(t, id)

	list, err := r.List(ctx, true)
	require.NoError(t, err)
	found := false
	for _, c := range list {
		if c.ID == id || (len(c.ID) >= 12 && len(id) >= 12 && c.ID[:12] == id[:12]) {
			found = true
		}
	}
	assert.True(t, found, "deployed container should appear in List")

	state, err := r.State(ctx, name)
	require.NoError(t, err)
	assert.True(t, state.Running)

	require.NoError(t, r.Remove(ctx, name, true, true))
	_, err = r.State(ctx, name)
	assert.Error(t, err)
}

func TestRepository_StopStartRestart(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	require.NoError(t, r.Stop(ctx, name, 5))
	state, err := r.State(ctx, name)
	require.NoError(t, err)
	assert.False(t, state.Running)

	require.NoError(t, r.Start(ctx, name))
	state, err = r.State(ctx, name)
	require.NoError(t, err)
	assert.True(t, state.Running)

	require.NoError(t, r.Restart(ctx, name))
	state, err = r.State(ctx, name)
	require.NoError(t, err)
	assert.True(t, state.Running)
}

func TestRepository_PauseUnpause(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	require.NoError(t, r.Pause(ctx, name))
	state, err := r.State(ctx, name)
	require.NoError(t, err)
	assert.True(t, state.Paused)

	require.NoError(t, r.Unpause(ctx, name))
	state, err = r.State(ctx, name)
	require.NoError(t, err)
	assert.False(t, state.Paused)
}

func TestRepository_Kill(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	require.NoError(t, r.Kill(ctx, name, "SIGKILL"))
	// Give the daemon a moment to reap the process.
	assert.Eventually(t, func() bool {
		state, err := r.State(ctx, name)
		return err == nil && !state.Running
	}, 5*time.Second, 100*time.Millisecond)
}

func TestRepository_Resources(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	res, err := r.Resources(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, int64(64*1024*1024), res.MemoryBytes)
}

func TestRepository_Stats(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	stats, err := r.Stats(ctx, name)
	require.NoError(t, err)
	assert.NotNil(t, stats)
}

func TestRepository_IP(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	ip, err := r.IP(ctx, name)
	require.NoError(t, err)
	assert.NotEmpty(t, ip)
}

func TestRepository_Logs(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	// Give the process a moment to write startup logs.
	time.Sleep(500 * time.Millisecond)
	logs, err := r.Logs(ctx, name, 10, false)
	require.NoError(t, err)
	assert.NotEmpty(t, logs)
}

func TestRepository_Logs_NotFound(t *testing.T) {
	r, ctx := testRepo(t)
	_, err := r.Logs(ctx, "agentbox-test-does-not-exist", 0, false)
	assert.ErrorIs(t, err, domain.ErrContainerNotFound)
}

func TestRepository_VolumePath(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	hostDir := t.TempDir()

	id, err := r.Run(ctx, domain.DeploySpec{
		Image:         testImage,
		Name:          name,
		MemoryLimit:   64 * 1024 * 1024,
		RestartPolicy: "no",
		Volumes: []domain.VolumeBind{
			{Type: "host", HostPath: hostDir, BindPath: "/data"},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Remove(context.Background(), name, true, true) })
	assert.NotEmpty(t, id)

	path, err := r.VolumePath(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, hostDir, path)
}

func TestRepository_VolumePath_NoBinds(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	runTestContainer(t, r, ctx, name)

	_, err := r.VolumePath(ctx, name)
	assert.ErrorIs(t, err, domain.ErrContainerNotFound)
}

func TestRepository_ImagesAndPull(t *testing.T) {
	r, ctx := testRepo(t)

	exists, err := r.ImageExists(ctx, testImage)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = r.ImageExists(ctx, "agentbox/definitely-not-a-real-image:latest")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, r.PullImage(ctx, testImage))

	images, err := r.ListImages(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, images)
}

func TestRepository_PullImage_InvalidTag(t *testing.T) {
	r, ctx := testRepo(t)
	err := r.PullImage(ctx, "agentbox/definitely-not-a-real-image:latest")
	assert.Error(t, err)
}

func TestRepository_NetworkLifecycle(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)

	require.NoError(t, r.CreateNetwork(ctx, name))
	t.Cleanup(func() { _ = r.DeleteNetwork(context.Background(), name) })

	networks, err := r.ListNetworks(ctx)
	require.NoError(t, err)
	assert.Contains(t, networks, name)

	require.NoError(t, r.DeleteNetwork(ctx, name))
	networks, err = r.ListNetworks(ctx)
	require.NoError(t, err)
	assert.NotContains(t, networks, name)
}

func TestRepository_VolumeLifecycle(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)

	require.NoError(t, r.CreateVolume(ctx, name))
	t.Cleanup(func() { _ = r.DeleteVolume(context.Background(), name) })

	volumes, err := r.ListVolumes(ctx)
	require.NoError(t, err)
	assert.Contains(t, volumes, name)

	require.NoError(t, r.DeleteVolume(ctx, name))
	volumes, err = r.ListVolumes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, volumes, name)
}

func TestRepository_Run_FullSpec(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	dockerVol := name + "-vol"
	hostDir := t.TempDir()

	require.NoError(t, r.CreateVolume(ctx, dockerVol))
	t.Cleanup(func() { _ = r.DeleteVolume(context.Background(), dockerVol) })

	id, err := r.Run(ctx, domain.DeploySpec{
		Image:         testImage,
		Name:          name,
		MemoryLimit:   64 * 1024 * 1024,
		SoftLimit:     32 * 1024 * 1024,
		CPUNano:       500_000_000,
		Env:           []string{"FOO=bar"},
		RestartPolicy: "no",
		Command:       []string{"redis-server", "--port", "6379"},
		Healthcheck: &domain.HealthcheckSpec{
			Test:            []string{"CMD-SHELL", "redis-cli ping || exit 1"},
			IntervalSeconds: 5,
			TimeoutSeconds:  2,
			Retries:         3,
		},
		Volumes: []domain.VolumeBind{
			{Type: "docker", VolumeName: dockerVol, BindPath: "/data"},
			{Type: "host", HostPath: hostDir, BindPath: "/hostdata", ReadOnly: true},
		},
		Ports: []domain.PortMapping{
			{ContainerPort: 6379, Protocol: "tcp"},
		},
		Networks: []string{"bridge"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Remove(context.Background(), name, true, true) })
	assert.NotEmpty(t, id)

	state, err := r.State(ctx, name)
	require.NoError(t, err)
	assert.True(t, state.Running)
}

func TestRepository_Run_InvalidPort(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)

	// ContainerPort out of the 0-65535 range makes nat.NewPort reject it
	// before any Docker API call is made.
	_, err := r.Run(ctx, domain.DeploySpec{
		Image:         testImage,
		Name:          name,
		MemoryLimit:   64 * 1024 * 1024,
		RestartPolicy: "no",
		Ports:         []domain.PortMapping{{ContainerPort: 70000, Protocol: "tcp"}},
	})
	assert.Error(t, err)
}

func TestRepository_Run_WithHostPort(t *testing.T) {
	r, ctx := testRepo(t)
	name := uniqueName(t)
	hostPort := 20000 + int(time.Now().UnixNano()%9999)

	id, err := r.Run(ctx, domain.DeploySpec{
		Image:         testImage,
		Name:          name,
		MemoryLimit:   64 * 1024 * 1024,
		RestartPolicy: "no",
		Ports:         []domain.PortMapping{{ContainerPort: 6379, HostPort: hostPort, Protocol: "tcp"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Remove(context.Background(), name, true, true) })
	assert.NotEmpty(t, id)
}

func TestNewRepository(t *testing.T) {
	r, _ := testRepo(t)
	assert.NotNil(t, r)
}
