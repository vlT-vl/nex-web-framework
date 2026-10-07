package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

const childEnv = "_NEXWEB_CHILD"

func MaybeDetach(version, publicAddr string) {
	if !Enabled() || os.Getenv(childEnv) != "" {
		return
	}
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), childEnv+"=1")
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatalf("nex-web: start daemon: %v", err)
	}
	if publicAddr != "" {
		fmt.Printf("nex-web %s started (PID %d) → http://localhost%s\n", version, cmd.Process.Pid, publicAddr)
	} else {
		fmt.Printf("nex-web %s started (PID %d)\n", version, cmd.Process.Pid)
	}
	os.Exit(0)
}
