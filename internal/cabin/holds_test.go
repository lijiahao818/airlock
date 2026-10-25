package cabin

import (
	"context"
	"testing"
)

// 运行页打开或刷新时，用 Holds 补上错过的拦截卡片。
func TestHolds(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	other := e.create(t, "codex")
	if got, err := e.m.Holds(c.ID); err != nil || len(got) != 0 {
		t.Fatalf("没有暂扣时 = %+v, %v", got, err)
	}
	res := make(chan bool, 2)
	ask := e.m.ask(c.ID, "r1", "Claude Code")
	go func() { res <- ask(context.Background(), heldEvent) }()
	h := *e.next(t, "hold").Hold
	second := heldEvent
	second.Host = "fake-second.example"
	go func() { res <- ask(context.Background(), second) }()
	h2 := *e.next(t, "hold").Hold

	got, err := e.m.Holds(c.ID)
	if err != nil || len(got) != 2 || got[0].ID > got[1].ID {
		t.Fatalf("Holds = %+v, %v", got, err)
	}
	if err := e.m.Decide(h2.ID, false); err != nil {
		t.Fatal(err)
	}
	result(t, res)
	got, _ = e.m.Holds(c.ID)
	if len(got) != 1 || got[0].ID != h.ID || got[0].Host != heldEvent.Host {
		t.Fatalf("决定一个后 = %+v", got)
	}
	// 其他工作舱看不到这个暂扣。
	if got, _ := e.m.Holds(other.ID); len(got) != 0 {
		t.Fatalf("其他工作舱的暂扣 = %+v", got)
	}
	if err := e.m.Decide(h.ID, false); err != nil {
		t.Fatal(err)
	}
	// 用户决定后，即使 ask 还没返回，暂扣也不再列出。
	if got, _ := e.m.Holds(c.ID); len(got) != 0 {
		t.Fatalf("决定后 = %+v", got)
	}
	result(t, res)
}

func TestHoldsUnknownCabin(t *testing.T) {
	e := newEnv(t, nil, nil)
	if _, err := e.m.Holds("fake-missing"); err == nil {
		t.Fatal("工作舱不存在时应该返回错误")
	}
}
