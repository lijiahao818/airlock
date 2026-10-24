//go:build !windows

package desktop

import (
	"context"
	"os/exec"
)

func urlCommand(string) (*exec.Cmd, error) { return nil, errUnsupported }

func terminalCommand([]string) (*exec.Cmd, error) { return nil, errUnsupported }

func pickFolder(context.Context) (string, error) { return "", errUnsupported }
