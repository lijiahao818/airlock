package console

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
)

func TestCabinHolds(t *testing.T) {
	e := newEnv(t)
	e.svc.holds = []cabin.Hold{{ID: "fake-hold", Cabin: "c1", HoldMeta: cabin.HoldMeta{Host: "fake.example", Port: 443}}}
	resp := e.request(t, http.MethodGet, "/api/cabins/c1/holds", "", fakeToken, "", "")
	got := body(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(got, `"id":"fake-hold"`) || !strings.Contains(got, `"host":"fake.example"`) {
		t.Fatalf("状态码 %d，响应 %s", resp.StatusCode, got)
	}
	if calls := e.svc.recorded(); len(calls) != 1 || calls[0] != `Holds "c1"` {
		t.Fatalf("调用记录 = %q", calls)
	}
}

// 没有暂扣时输出 []，不输出 null。
func TestCabinHoldsEmpty(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/api/cabins/c1/holds", "", fakeToken, "", "")
	if got := strings.TrimSpace(body(t, resp)); got != "[]" {
		t.Fatalf("响应 = %s", got)
	}
}

func TestCabinHoldsRejects(t *testing.T) {
	t.Run("没有令牌", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/holds", "", "", "", "")
		body(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || len(e.svc.recorded()) != 0 {
			t.Fatalf("状态码 %d，调用记录 %q", resp.StatusCode, e.svc.recorded())
		}
	})
	t.Run("令牌错误", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/holds", "", "fake-wrong-token", "", "")
		body(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || len(e.svc.recorded()) != 0 {
			t.Fatalf("状态码 %d，调用记录 %q", resp.StatusCode, e.svc.recorded())
		}
	})
	t.Run("工作舱不存在", func(t *testing.T) {
		e := newEnv(t)
		e.svc.holdsErr = errors.New("cabin: 没有这个工作舱。请回到工作舱列表。")
		resp := e.request(t, http.MethodGet, "/api/cabins/fake-missing/holds", "", fakeToken, "", "")
		got := body(t, resp)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(got, "没有这个工作舱") || strings.Contains(got, "cabin:") {
			t.Fatalf("状态码 %d，响应 %s", resp.StatusCode, got)
		}
	})
}
