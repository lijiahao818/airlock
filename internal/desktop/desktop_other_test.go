//go:build !windows

package desktop

import (
	"context"
	"errors"
	"testing"
)

// 非 Windows 平台上，合法的调用也全部返回错误。
func TestUnsupported(t *testing.T) {
	got := recordStart(t)
	if err := OpenURL("http://127.0.0.1/"); !errors.Is(err, errUnsupported) {
		t.Errorf("OpenURL = %v", err)
	}
	if err := OpenTerminal([]string{"docker", "ps"}); !errors.Is(err, errUnsupported) {
		t.Errorf("OpenTerminal = %v", err)
	}
	if _, err := PickFolder(context.Background()); !errors.Is(err, errUnsupported) {
		t.Errorf("PickFolder = %v", err)
	}
	if len(*got) != 0 {
		t.Fatal("不能启动进程")
	}
}
