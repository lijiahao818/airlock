//go:build !windows

package vault

import "errors"

var errUnsupported = errors.New("vault: 只支持 Windows 凭据管理器")

func write(target string, secret []byte) error { return errUnsupported }

func read(target string) ([]byte, error) { return nil, errUnsupported }

func remove(target string) error { return errUnsupported }
