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
