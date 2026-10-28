package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"nex-web/internal/hostenv"
)

const childEnv = hostenv.ChildEnv

func MaybeDetach(name, version, publicAddr string) {
	hostenv.Recover(name)
	if !Enabled() || os.Getenv(childEnv) != "" || hostenv.UpdaterLaunched() {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		executable = os.Args[0]
	}
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), childEnv+"=1")
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatalf("nex-web: start daemon: %v", err)
	}
	if publicAddr != "" {
		fmt.Printf("%s %s started (PID %d) → http://localhost%s\n", name, version, cmd.Process.Pid, publicAddr)
	} else {
		fmt.Printf("%s %s started (PID %d)\n", name, version, cmd.Process.Pid)
	}
	os.Exit(0)
}
