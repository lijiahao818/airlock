package cabin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

func problemOf(t *testing.T, e *env, id string) string {
	t.Helper()
	list, _ := e.m.List()
	for _, c := range list {
		if c.ID == id {
			return c.Problem
		}
	}
	t.Fatalf("没有工作舱 %s", id)
	return ""
}

// 沙箱或出网中继停止时，List 给出中文说明，运行页显示它（#84）。
func TestWatchReportsProblem(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.m.watchEvery = 10 * time.Millisecond
	c := e.started(t, "claude-code")
	time.Sleep(50 * time.Millisecond)
	if p := problemOf(t, e, c.ID); p != "" {
		t.Fatalf("一切正常时 Problem = %q", p)
	}
	// docker 暂时没有响应时不误报。
	e.b.setHealth(errors.New("fake: docker 没有响应"))
	time.Sleep(50 * time.Millisecond)
	if p := problemOf(t, e, c.ID); p != "" {
		t.Fatalf("检查失败时 Problem = %q", p)
	}
	e.b.setHealth(fakeStopped)
	deadline := time.Now().Add(2 * time.Second)
	for problemOf(t, e, c.ID) == "" {
		if time.Now().After(deadline) {
			t.Fatal("出网中继停止后，List 没有给出说明")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p := problemOf(t, e, c.ID); p != fakeStopped.Error() {
		t.Fatalf("Problem = %q", p)
	}
	// 同时发出 problem 事件，运行页不用等下一次轮询。
	if ev := e.next(t, "problem"); ev.Cabin != c.ID || ev.Problem != fakeStopped.Error() {
		t.Fatalf("problem 事件 = %+v", ev)
	}
	// 停止后进入出舱审查，问题随运行一起结束。
	if err := e.m.Stop(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if p := problemOf(t, e, c.ID); p != "" {
		t.Fatalf("停止后 Problem = %q", p)
	}
}

// 运行结束后不再检查。
func TestWatchStopsAfterRun(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.m.watchEvery = 10 * time.Millisecond
	c := e.started(t, "claude-code")
	if err := e.m.Stop(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	n := e.b.called("health")
	time.Sleep(100 * time.Millisecond)
	if e.b.called("health") != n {
		t.Fatalf("运行结束后仍在检查：%d 次后又有 %d 次", n, e.b.called("health")-n)
	}
}

var fakeStopped = &sandbox.StoppedError{Msg: "出网中继已经停止，agent 不能再连接网络。"}
