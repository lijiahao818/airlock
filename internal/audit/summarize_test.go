package audit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/gate"
)

func routeEntry(provider string, rule string, allowed bool) Entry {
	return Entry{Gate: &gate.Event{Time: fakeTime, Host: "api." + provider + ".com", Port: 443, Method: "POST", Rule: rule, Allowed: allowed, Provider: provider}}
}

func TestSummarizeHosts(t *testing.T) {
	r := Summarize([]Entry{
		gateEntry("fake-b.example", "tier", true),
		gateEntry("fake-a.example", "user-deny", false),
		gateEntry("fake-b.example", "tier", true),
		gateEntry("fake-a.example", "run-deny", false),
		gateEntry("fake-b.example", "private-address", false),
	})
	want := []HostSummary{
		{Host: "fake-b.example", Port: 443, Allowed: 2, Denied: 1},
		{Host: "fake-a.example", Port: 443, Denied: 2},
	}
	if !reflect.DeepEqual(r.Hosts, want) {
		t.Fatalf("Hosts = %+v\nwant %+v", r.Hosts, want)
	}
}

func TestSummarizeHolds(t *testing.T) {
	r := Summarize([]Entry{
		gateEntry("fake-a.example", "user-deny", false),
		gateEntry("fake-a.example", "run-deny", false),
		gateEntry("fake-b.example", "timeout", false),
		gateEntry("fake-c.example", "user-allow", true),
		gateEntry("fake-c.example", "run-allow", true),
		gateEntry("fake-d.example", "tier", true),
		gateEntry("fake-e.example", "bad-host", false),
	})
	want := []HoldSummary{
		{Host: "fake-a.example", Port: 443, Rule: "user-deny"},
		{Host: "fake-b.example", Port: 443, Rule: "timeout"},
		{Host: "fake-c.example", Port: 443, Rule: "user-allow"},
	}
	if !reflect.DeepEqual(r.Holds, want) {
		t.Fatalf("Holds = %+v\nwant %+v", r.Holds, want)
	}
}

// 只有换上了真实密钥的 route 事件才算一次密钥使用。
func TestSummarizeKeyUses(t *testing.T) {
	r := Summarize([]Entry{
		routeEntry("anthropic", "route", true),
		routeEntry("anthropic", "route", true),
		routeEntry("deepseek", "route", true),
		routeEntry("openai", "bad-placeholder", false),
		routeEntry("openai", "no-key", false),
		{Gate: &gate.Event{Host: "api.openai.com", Rule: "route", Allowed: false}},
	})
	if want := map[string]int{"anthropic": 2, "deepseek": 1}; !reflect.DeepEqual(r.KeyUses, want) {
		t.Fatalf("KeyUses = %v, want %v", r.KeyUses, want)
	}
}

func TestSummarizeFiles(t *testing.T) {
	r := Summarize([]Entry{
		reviewEntry("a.txt", true),
		gateEntry("fake-a.example", "tier", true),
		reviewEntry("b.txt", false),
	})
	want := []Review{*reviewEntry("a.txt", true).Review, *reviewEntry("b.txt", false).Review}
	if !reflect.DeepEqual(r.Files, want) {
		t.Fatalf("Files = %+v\nwant %+v", r.Files, want)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	for _, entries := range [][]Entry{nil, {}, {{}}} {
		r := Summarize(entries)
		if r.Hosts == nil || r.Holds == nil || r.KeyUses == nil || r.Files == nil {
			t.Fatalf("Summarize(%v) 有 nil 字段：%+v", entries, r)
		}
		data, _ := json.Marshal(r)
		if want := `{"hosts":[],"holds":[],"keyUses":{},"files":[]}`; string(data) != want {
			t.Fatalf("JSON = %s, want %s", data, want)
		}
	}
}

// 汇总结果中只有主机、规则、供应商和文件路径，没有请求路径等其他字段。
func TestSummarizeKeepsOnlyMetadata(t *testing.T) {
	e := routeEntry("anthropic", "route", true)
	e.Gate.Path = "/anthropic/v1/fake-secret-path"
	data, _ := json.Marshal(Summarize([]Entry{e}))
	if strings.Contains(string(data), "fake-secret-path") {
		t.Fatalf("运行报告中有请求路径：%s", data)
	}
}

// 同一主机的不同端口分开统计，报告中能看出 agent 连接了哪些端口（#73）。
func TestSummarizeSplitsPorts(t *testing.T) {
	https := gateEntry("fake-a.example", "user-allow", true)
	ssh := gateEntry("fake-a.example", "user-deny", false)
	ssh.Gate.Port = 22
	r := Summarize([]Entry{https, ssh})
	wantHosts := []HostSummary{
		{Host: "fake-a.example", Port: 443, Allowed: 1},
		{Host: "fake-a.example", Port: 22, Denied: 1},
	}
	wantHolds := []HoldSummary{
		{Host: "fake-a.example", Port: 443, Rule: "user-allow"},
		{Host: "fake-a.example", Port: 22, Rule: "user-deny"},
	}
	if !reflect.DeepEqual(r.Hosts, wantHosts) || !reflect.DeepEqual(r.Holds, wantHolds) {
		t.Fatalf("Hosts = %+v\nHolds = %+v", r.Hosts, r.Holds)
	}
}
