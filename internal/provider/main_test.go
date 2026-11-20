package provider

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/spawn"
)

// The test binary also plays the processes the tests start.
func TestMain(m *testing.M) {
	switch {
	case os.Getenv("AIDE_TEST_SPAWN_HELPER") == "1":
		spawn.Serve(os.NewFile(3, "helper"))
		os.Exit(0)
	case os.Getenv("AIDE_TEST_TREE") == "parent":
		// An agent with a tool: it starts a child, says which, and waits.
		exe, _ := os.Executable()
		c := exec.Command(exe, "-test.run=^$")
		c.Env = append(os.Environ(), "AIDE_TEST_TREE=child")
		if err := c.Start(); err != nil {
			fmt.Println("error", err)
			os.Exit(1)
		}
		fmt.Println("child", c.Process.Pid)
		time.Sleep(time.Hour)
		os.Exit(0)
	case os.Getenv("AIDE_TEST_TREE") == "child":
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
