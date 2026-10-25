package cabin

import (
	"context"
	"testing"
	"time"
)

// 程序退出时停止全部运行中的工作舱。它们进入出舱审查，重启后可以继续审查。
func TestStopAll(t *testing.T) {
	e := newEnv(t, nil, nil)
	a := e.started(t, "claude-code")
	b := e.started(t, "codex")
	idle := e.create(t, "claude-code")
	if err := e.m.StopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.b.called("remove") != 2 {
		t.Errorf("删除沙箱 %d 次，want 2", e.b.called("remove"))
	}
	for _, c := range []Cabin{a, b} {
		if e.status(t, c.ID) != Review {
			t.Errorf("%s 的状态 = %q", c.ID, e.status(t, c.ID))
		}
	}
	if e.status(t, idle.ID) != Idle {
		t.Errorf("空闲的工作舱状态 = %q", e.status(t, idle.ID))
	}
	m2, err := New(Options{DataDir: e.dataDir, Backend: &fakeBackend{}, Secret: e.m.o.Secret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.ReviewItems(a.ID); err != nil {
		t.Fatalf("重启后不能审查：%v", err)
	}
}

// Docker 不响应时，StopAll 在 ctx 到期后返回，程序可以退出。
func TestStopAllTimeout(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.b.onRemove = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := e.m.StopAll(ctx); err == nil {
		t.Fatal("超时后应该返回错误")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("StopAll 用了 %v", d)
	}
	// 沙箱没有删掉，状态不变。下次启动时 New 把它改为 Review，Prune 删除残留的容器。
	if e.status(t, c.ID) != Running {
		t.Errorf("状态 = %q", e.status(t, c.ID))
	}
}
