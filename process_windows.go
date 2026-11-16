//go:build windows

package main

import (
	"os"
	"os/exec"
)

func configureDetached(cmd *exec.Cmd) {}

func configureProcessGroup(cmd *exec.Cmd) {}

func signalProcess(pid int, force bool) {
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Kill()
}

func signalProcessGroup(pid int, force bool) {
	signalProcess(pid, force)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process != nil
}
