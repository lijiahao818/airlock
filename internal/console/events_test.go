package console

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/cabin"
)

// waitCancel 在两秒内等待 cancel 被调用。
func waitCancel(t *testing.T, f *fakeService) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.cancel.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("两秒内没有调用 cancel")
}

// AC-6：/api/events 以 text/event-stream 输出事件，客户端断开时调用 cancel。
func TestEvents(t *testing.T) {
	e := newEnv(t)
	pushed := []cabin.Event{
		{Kind: "status", Cabin: "c1", Status: cabin.Running},
		{Kind: "progress", Cabin: "c1", Progress: &cabin.Progress{Done: 1, Total: 2}},
	}
	for _, ev := range pushed {
		e.svc.events <- ev
	}
	resp := e.request(t, http.MethodGet, "/api/events", "", fakeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q，want text/event-stream", ct)
	}
	if got := strings.Join(e.svc.recorded(), ";"); got != "Subscribe" {
		t.Errorf("调用记录 = %q", got)
	}

	rd := bufio.NewReader(resp.Body)
	for _, want := range pushed {
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatalf("读事件行：%v", err)
		}
		if line != "data: "+string(wantJSON)+"\n" {
			t.Errorf("事件行 = %q，want %q", line, "data: "+string(wantJSON)+"\n")
		}
		blank, err := rd.ReadString('\n')
		if err != nil || blank != "\n" {
			t.Errorf("事件之间应该是空行：%q，%v", blank, err)
		}
	}

	// 客户端断开后调用 cancel。
	resp.Body.Close()
	waitCancel(t, e.svc)
}

// 没有事件时也立即发送响应头，前端的 fetch 不用等第一个事件。
func TestEventsHeadersWithoutEvents(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/api/events", "", fakeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q，want text/event-stream", ct)
	}
	resp.Body.Close()
	waitCancel(t, e.svc)
}

// 事件通道被关闭时，连接正常结束。
func TestEventsClosedChannel(t *testing.T) {
	e := newEnv(t)
	close(e.svc.events)
	resp := e.request(t, http.MethodGet, "/api/events", "", fakeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q，want text/event-stream", ct)
	}
	if _, err := resp.Body.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("通道关闭后应该结束流：%v", err)
	}
	resp.Body.Close()
	waitCancel(t, e.svc)
}

// 事件流和别的 /api 请求一样需要令牌。
func TestEventsNeedToken(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/api/events", "", "", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("状态码 = %d，want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	body(t, resp)
	if got := e.svc.recorded(); len(got) != 0 {
		t.Errorf("没有令牌时不应调用 Subscribe：%q", got)
	}
}
