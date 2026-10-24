//go:build !windows

package desktop

import "os/exec"

func urlCommand(string) (*exec.Cmd, error) { return nil, errUnsupported }

func terminalCommand([]string) (*exec.Cmd, error) { return nil, errUnsupported }
