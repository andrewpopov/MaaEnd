//go:build !windows

package accountscan

import "os/exec"

func configureCommand(command *exec.Cmd) {}
