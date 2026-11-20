package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/internal/connaddr"
)

// TestLinuxMachineInDocker pairs a Linux hermeshd running in a container
// (standing in for a VPS) and runs a session on it. HERMESH_DOCKER=1.
func TestLinuxMachineInDocker(t *testing.T) {
	if os.Getenv("HERMESH_DOCKER") != "1" {
		t.Skip("set HERMESH_DOCKER=1")
	}
	bin := binaries(t)
	tmp := t.TempDir()
	linuxBin := filepath.Join(tmp, "linux")
	os.MkdirAll(linuxBin, 0o755)
	cmd := exec.Command("go", "build", "-o", filepath.Join(linuxBin, "hermeshd"), "./cmd/hermeshd")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	ctx := context.Background()
	port := freePort(t)
	relayData := filepath.Join(tmp, "relay")
	relay := start(t, "relay", nil, filepath.Join(bin, "hermesh-relay"), "serve", "--data-dir", relayData, "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	eventually(t, 20*time.Second, "relay up", func() bool { return strings.Contains(relay.output(), "relay listening") })
	claim := strings.TrimSpace(run(t, nil, 10*time.Second, filepath.Join(bin, "hermesh-relay"), "setup-code", "--data-dir", relayData).stdout)
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "o.sock")
	start(t, "owner", nil, filepath.Join(bin, "hermeshd"), "run", "--owner", "--data-dir", filepath.Join(tmp, "owner"), "--socket", sock)
	app := newTestApp(t, sock)
	setup, err := app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: fmt.Sprintf("http://127.0.0.1:%d", port), SetupCode: claim, Account: testAccount, Password: testPassword, Label: "test-mac"})
	if err != nil {
		t.Fatal(err)
	}
	containerAddr := strings.Replace(connaddr.Format(fmt.Sprintf("http://127.0.0.1:%d", port), setup.OwnerId), "127.0.0.1", "host.docker.internal", 1)

	name := fmt.Sprintf("hermesh-e2e-%d", time.Now().UnixNano())
	script := strings.Join([]string{
		"apk add --no-cache git >/dev/null",
		"git config --global user.email t@t && git config --global user.name t",
		"mkdir -p /srv/demo && cd /srv/demo && git init -q -b main && echo hi > README.md && git add -A && git commit -qm init",
		"/opt/hermesh/hermeshd login " + containerAddr + " --name linux-vps --no-service --data-dir /data",
		"exec /opt/hermesh/hermeshd run --data-dir /data",
	}, " && ")
	dockerRun := exec.Command("docker", "run", "--rm", "-d", "--name", name, "--add-host", "host.docker.internal:host-gateway",
		"-v", linuxBin+":/opt/hermesh:ro", "-e", "HERMESH_FAKE_PROVIDERS=1", "-e", "AIDE_ACCOUNT="+testAccount, "-e", "AIDE_PASSWORD="+testPassword, "alpine:3", "sh", "-c", script)
	if out, err := dockerRun.CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	logs := func() string { o, _ := exec.Command("docker", "logs", name).CombinedOutput(); return string(o) }
	var machineID string
	eventually(t, 90*time.Second, "the linux machine logged in", func() bool {
		machineID = loggedInMachine(logs())
		return machineID != ""
	})
	eventually(t, 60*time.Second, "linux machine online", func() bool {
		res, err := app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return false
		}
		for _, n := range res.Machines {
			if n.MachineId == machineID && n.Online && n.Status != nil {
				return true
			}
		}
		return false
	})
	if _, err := app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: machineID, ProjectId: "demo", Path: "/srv/demo"}); err != nil {
		t.Fatalf("add project: %v\n%s", err, logs())
	}
	home := []string{"HERMESH_HOME=" + filepath.Join(tmp, "agent")}
	login := start(t, "login", append(slices.Clone(home), loginEnv()...), filepath.Join(bin, "hermesh"), "auth", "login", connaddr.Format(fmt.Sprintf("http://127.0.0.1:%d", port), setup.OwnerId), "--name", "agent")
	login.wait(t, 30*time.Second)
	r := run(t, home, 2*time.Minute, filepath.Join(bin, "hermesh"), "session", "start", "--project", "linux-vps/demo", "--wait", "-o", "json",
		"--prompt", "write from-linux.txt hello\nsay done")
	if r.code != 0 {
		t.Fatalf("session: %+v\n%s", r, logs())
	}
	if !strings.Contains(r.stdout, "idle") {
		t.Fatalf("session result: %s", r.stdout)
	}
}
