package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockDir 锁住数据目录。锁是一个不允许共享打开的文件句柄，进程退出时 Windows 自动关闭它，
// 所以进程异常退出后锁也会释放。另一个进程已经持有锁时，返回 errHeld。
func lockDir(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(filepath.Join(dir, "airlock.lock"))
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, errSharingViolation) {
		return nil, errHeld
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), "airlock.lock"), nil
}

// errSharingViolation 是 ERROR_SHARING_VIOLATION。syscall 包没有定义这个常量。
const errSharingViolation syscall.Errno = 32
