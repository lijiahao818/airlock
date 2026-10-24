//go:build !windows

package vault

import (
	"errors"
	"testing"
)

// 非 Windows 平台上，合法的调用也全部返回错误。
func TestUnsupported(t *testing.T) {
	if err := Set("fake-provider", fakeSecret); !errors.Is(err, errUnsupported) {
		t.Errorf("Set = %v", err)
	}
	if _, err := Get("fake-provider"); !errors.Is(err, errUnsupported) {
		t.Errorf("Get = %v", err)
	}
	if err := Delete("fake-provider"); !errors.Is(err, errUnsupported) {
		t.Errorf("Delete = %v", err)
	}
	if _, err := Hint("fake-provider"); !errors.Is(err, errUnsupported) {
		t.Errorf("Hint = %v", err)
	}
}
