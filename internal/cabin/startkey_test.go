package cabin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 供应商没有密钥时，启动在构建镜像前失败，不创建沙箱，工作舱仍为空闲。
func TestStartNeedsKey(t *testing.T) {
	for name, secret := range map[string]func(string) (string, error){
		"取不到密钥": func(string) (string, error) { return "", errors.New("fake: 没有找到密钥") },
		"密钥为空":  func(string) (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.m.o.Secret = secret
			c := e.create(t, "codex")
			err := e.m.Start(context.Background(), c.ID)
			if err == nil || !strings.Contains(err.Error(), "OpenAI 的密钥") || !strings.Contains(err.Error(), "密钥”页") {
				t.Fatalf("Start = %v", err)
			}
			if e.b.called("create") != 0 || e.status(t, c.ID) != Idle {
				t.Fatalf("创建了 %d 个沙箱，状态 %q", e.b.called("create"), e.status(t, c.ID))
			}
		})
	}
}

// 密钥存在时，启动流程不变。检查只看有没有密钥，沙箱中仍然只有占位符。
func TestStartWithKey(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "codex")
	if e.status(t, c.ID) != Running || e.b.called("create") != 1 {
		t.Fatalf("状态 %q，创建了 %d 个沙箱", e.status(t, c.ID), e.b.called("create"))
	}
	for k, v := range e.b.lastSpec(t).Env {
		if strings.Contains(v, fakeSecret) {
			t.Fatalf("沙箱的环境变量 %s 中有真实密钥", k)
		}
	}
}
