package main

import (
	"errors"
	"testing"
)

func TestLockDir(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 已有进程持有锁。
	if f, err := lockDir(dir); !errors.Is(err, errHeld) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("第二次加锁 = %v，want errHeld", err)
	}
	// 锁已释放。进程退出时系统关闭句柄，效果相同。
	first.Close()
	again, err := lockDir(dir)
	if err != nil {
		t.Fatalf("释放后加锁 = %v", err)
	}
	again.Close()
}
