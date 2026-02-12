//go:build windows

package main

import "os/exec"

func setProcGroup(cmd *exec.Cmd) {
	// Windows doesn't support Unix process groups
}

func terminateProcess(cmd *exec.Cmd) {
	cmd.Process.Kill()
}

func forceKillProcess(cmd *exec.Cmd) {
	cmd.Process.Kill()
}
