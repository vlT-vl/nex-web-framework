// Package daemon handles self-daemonization of the nex-web binary.
//
// In release builds (no -tags dev), calling MaybeDetach re-executes the
// current binary as a detached child process, prints the listening address,
// and exits the parent. The child re-enters main() with _NEXWEB_CHILD=1 set
// and MaybeDetach returns normally so the server can start.
//
// In dev builds (-tags dev), Enabled() returns false and MaybeDetach is a no-op.
package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

const childEnv = "_NEXWEB_CHILD"

// MaybeDetach forks a detached copy of the current process when daemonization
// is enabled (release build) and this is not already the daemon child.
// The parent prints the public URL and exits; the child returns normally.
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
