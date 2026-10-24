package sandbox

import (
	"context"
	"testing"
)

// Spec.Name 只能含小写字母、数字和连字符。其他名称必须在调用 docker 前被拒绝。
func TestCreateRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "Bad", "a b", "a;rm -rf /", "../x"} {
		if id, err := Docker().Create(context.Background(), Spec{Name: name}); err == nil || id != "" {
			t.Errorf("Create(%q) = %q, %v; want error", name, id, err)
		}
	}
}

// RemoveCache 的 name 会拼进卷名。非法名称必须在调用 docker 前被拒绝。
func TestRemoveCacheRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "Bad", "a b", "a;rm -rf /", "../x"} {
		if err := Docker().RemoveCache(context.Background(), name); err == nil {
			t.Errorf("RemoveCache(%q) 没有报错", name)
		}
	}
}
