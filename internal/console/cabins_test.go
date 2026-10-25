package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/staging"
)

// AC-1：每个工作舱路由调用 cabin.Service 的对应方法，参数来自路径和请求体。
func TestCabinRoutes(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		body   string
		want   string
	}{
		{"列表", http.MethodGet, "/api/cabins", "", "List"},
		{"创建", http.MethodPost, "/api/cabins",
			`{"project":"D:\\fake-project","preset":"claude-code","tier":"standard"}`,
			`Create "D:\\fake-project" "claude-code" "standard"`},
		{"启动", http.MethodPost, "/api/cabins/c1/start", "", `Start "c1"`},
		{"停止", http.MethodPost, "/api/cabins/c1/stop", "", `Stop "c1"`},
		{"重置", http.MethodPost, "/api/cabins/c1/reset", "", `Reset "c1"`},
		{"删除", http.MethodPost, "/api/cabins/c1/delete", "", `Delete "c1"`},
		{"agent 网址", http.MethodGet, "/api/cabins/c1/agent-url", "", `AgentURL "c1"`},
		{"打开终端", http.MethodPost, "/api/cabins/c1/terminal", "", `OpenTerminal "c1"`},
		{"进舱检查", http.MethodGet, "/api/cabins/c1/entry", "", `EntryFindings "c1"`},
		{"改动列表", http.MethodGet, "/api/cabins/c1/changes", "", `ReviewItems "c1"`},
		{"改动内容", http.MethodGet, "/api/cabins/c1/diff?path=sub%2Fa+b.go", "", `Diff "c1" "sub/a b.go"`},
		{"撤销改动", http.MethodPost, "/api/cabins/c1/revert", `{"path":"main.go"}`, `Revert "c1" "main.go"`},
		{"应用改动", http.MethodPost, "/api/cabins/c1/apply", `{"path":"main.go"}`, `Apply "c1" "main.go"`},
		{"完成审查", http.MethodPost, "/api/cabins/c1/finish", "", `FinishReview "c1"`},
		{"运行报告", http.MethodGet, "/api/cabins/c1/report", "", `Report "c1"`},
		{"放行拦截", http.MethodPost, "/api/holds/hold-1", `{"allow":true}`, `Decide "hold-1" true`},
		{"拒绝拦截", http.MethodPost, "/api/holds/hold-1", `{"allow":false}`, `Decide "hold-1" false`},
		{"没有决定字段时拒绝", http.MethodPost, "/api/holds/hold-1", `{}`, `Decide "hold-1" false`},
		{"撤销决定", http.MethodPost, "/api/cabins/c1/reset-decision", `{"host":"api.example.com"}`,
			`ResetDecision "c1" "api.example.com"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			resp := e.request(t, tt.method, tt.target, tt.body, fakeToken, "", "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
			}
			body(t, resp)
			if got := strings.Join(e.svc.recorded(), ";"); got != tt.want {
				t.Errorf("调用记录 = %q，want %q", got, tt.want)
			}
		})
	}
}

// 工作舱路由返回依赖给出的数据。
func TestCabinData(t *testing.T) {
	t.Run("创建", func(t *testing.T) {
		e := newEnv(t)
		e.svc.createCabin = cabin.Cabin{
			ID: "c1", Name: "fake-project", Project: `D:\fake-project`,
			Preset: "claude-code", Tier: "standard", Status: cabin.Idle,
		}
		resp := e.request(t, http.MethodPost, "/api/cabins", `{}`, fakeToken, "", "")
		want := `{"id":"c1","name":"fake-project","project":"D:\\fake-project","preset":"claude-code","tier":"standard","status":"idle"}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("列表", func(t *testing.T) {
		e := newEnv(t)
		e.svc.list = []cabin.Cabin{{ID: "c1", Name: "fake-project", Status: cabin.Running, RunID: "run-1"}}
		resp := e.request(t, http.MethodGet, "/api/cabins", "", fakeToken, "", "")
		want := `[{"id":"c1","name":"fake-project","project":"","preset":"","tier":"","status":"running","runId":"run-1"}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("agent 网址", func(t *testing.T) {
		e := newEnv(t)
		e.svc.agentURLValue = "http://127.0.0.1:1/?cabin=c1#token=fake-term"
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/agent-url", "", fakeToken, "", "")
		want := `{"url":"http://127.0.0.1:1/?cabin=c1#token=fake-term"}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("进舱检查", func(t *testing.T) {
		e := newEnv(t)
		e.svc.entryFindings = []cabin.Finding{{Path: ".env", Kind: cabin.Excluded, Reason: "里面有密钥"}}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/entry", "", fakeToken, "", "")
		want := `[{"path":".env","kind":"excluded","reason":"里面有密钥"}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("改动列表", func(t *testing.T) {
		e := newEnv(t)
		e.svc.reviewItems = []cabin.ReviewItem{{
			Change:   staging.Change{Path: "main.go", Kind: staging.Modified},
			Findings: []cabin.Finding{{Path: "main.go", Kind: cabin.HighRisk, Reason: "改了可执行文件"}},
		}}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/changes", "", fakeToken, "", "")
		want := `[{"path":"main.go","kind":"modified","conflict":false,"special":false,"applied":false,` +
			`"findings":[{"path":"main.go","kind":"high-risk","reason":"改了可执行文件"}]}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("没有发现的改动输出空数组", func(t *testing.T) {
		e := newEnv(t)
		e.svc.reviewItems = []cabin.ReviewItem{{
			Change:   staging.Change{Path: "a.go", Kind: staging.Added},
			Findings: nil,
		}}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/changes", "", fakeToken, "", "")
		want := `[{"path":"a.go","kind":"added","conflict":false,"special":false,"applied":false,"findings":[]}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("运行报告", func(t *testing.T) {
		e := newEnv(t)
		e.svc.reportValue = audit.Report{
			Hosts:   []audit.HostSummary{{Host: "api.anthropic.com", Port: 443, Allowed: 3}},
			Holds:   []audit.HoldSummary{{Host: "evil.com", Port: 443, Rule: "user-deny"}},
			KeyUses: map[string]int{"anthropic": 3},
			Files:   []audit.Review{{Path: "main.go", Applied: true}},
		}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/report", "", fakeToken, "", "")
		want := `{"hosts":[{"host":"api.anthropic.com","port":443,"allowed":3,"denied":0}],` +
			`"holds":[{"host":"evil.com","port":443,"rule":"user-deny"}],` +
			`"keyUses":{"anthropic":3},"files":[{"time":"0001-01-01T00:00:00Z","path":"main.go","applied":true}]}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})
}

// 依赖返回错误时，路由返回 400，并把错误原文带给用户。
func TestCabinErrors(t *testing.T) {
	e := newEnv(t)
	e.svc.revertErr = errFakeRevert
	resp := e.request(t, http.MethodPost, "/api/cabins/c1/revert", `{"path":"main.go"}`, fakeToken, "", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if got := body(t, resp); !strings.Contains(got, errFakeRevert.Error()) {
		t.Errorf("响应 = %q，应包含原始错误", got)
	}
}

// AC-5：列表字段没有元素时输出 []，不输出 null。
func TestEmptyLists(t *testing.T) {
	cases := map[string]string{
		"/api/cabins":                   "[]",
		"/api/cabins/c1/entry":          "[]",
		"/api/cabins/c1/changes":        "[]",
		"/api/cabins/c1/diff?path=a.go": `{"lines":[]}`,
		"/api/cabins/c1/report":         `{"hosts":[],"holds":[],"keyUses":{},"files":[]}`,
	}
	for target, want := range cases {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, target, "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s：状态码 = %d", target, resp.StatusCode)
		}
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("%s：响应 = %s，want %s", target, got, want)
		}
	}
}

// #98：改动内容原样输出。不显示内容时输出原因。staging 拒绝的路径返回 400 和中文原因。
func TestDiffRoute(t *testing.T) {
	t.Run("新增和删除的行", func(t *testing.T) {
		e := newEnv(t)
		e.svc.diffValue = staging.Diff{Lines: []staging.Line{
			{Op: staging.OpDel, Text: "fake-old"},
			{Op: staging.OpAdd, Text: `<script>alert(1)</script> https://evil.example`},
			{Op: staging.OpSkip, Skip: 4},
		}}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/diff?path=a.go", "", fakeToken, "", "")
		var got staging.Diff
		if err := json.Unmarshal([]byte(body(t, resp)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, e.svc.diffValue) {
			t.Errorf("响应 = %+v，want %+v", got, e.svc.diffValue)
		}
	})

	t.Run("不显示内容的原因", func(t *testing.T) {
		e := newEnv(t)
		e.svc.diffValue = staging.Diff{Reason: "fake-原因"}
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/diff?path=a.bin", "", fakeToken, "", "")
		want := `{"lines":[],"reason":"fake-原因"}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("拒绝的路径", func(t *testing.T) {
		e := newEnv(t)
		e.svc.diffErr = errors.New("staging: 路径不合法")
		resp := e.request(t, http.MethodGet, "/api/cabins/c1/diff?path=..%2Fsecret", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
		}
		if got := strings.TrimSpace(body(t, resp)); got != "路径不合法" {
			t.Errorf("响应 = %q", got)
		}
		if got := strings.Join(e.svc.recorded(), ";"); got != `Diff "c1" "../secret"` {
			t.Errorf("调用记录 = %q", got)
		}
	})
}

// #80 AC-3：cabin 拒绝删除时，路由返回 400 和中文原因。
func TestDeleteRejected(t *testing.T) {
	e := newEnv(t)
	e.svc.deleteErr = errors.New("cabin: 工作舱现在是“运行中”状态，不能删除。请先停止运行。")
	resp := e.request(t, http.MethodPost, "/api/cabins/c1/delete", "", fakeToken, "", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if got := strings.TrimSpace(body(t, resp)); got != "工作舱现在是“运行中”状态，不能删除。请先停止运行。" {
		t.Errorf("响应 = %q", got)
	}
}

// 每个读数据的路由在依赖返回错误时都返回 400，并带上错误原文。
func TestCabinReadErrors(t *testing.T) {
	cases := map[string]func(f *fakeService){
		"/api/cabins":                   func(f *fakeService) { f.listErr = errFakeRevert },
		"/api/cabins/c1/agent-url":      func(f *fakeService) { f.agentURLErr = errFakeRevert },
		"/api/cabins/c1/entry":          func(f *fakeService) { f.entryErr = errFakeRevert },
		"/api/cabins/c1/changes":        func(f *fakeService) { f.reviewErr = errFakeRevert },
		"/api/cabins/c1/report":         func(f *fakeService) { f.reportErr = errFakeRevert },
		"/api/cabins/c1/diff?path=a.go": func(f *fakeService) { f.diffErr = errFakeRevert },
	}
	for target, set := range cases {
		e := newEnv(t)
		set(e.svc)
		resp := e.request(t, http.MethodGet, target, "", fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s：状态码 = %d，want %d", target, resp.StatusCode, http.StatusBadRequest)
		}
		if got := body(t, resp); !strings.Contains(got, errFakeRevert.Error()) {
			t.Errorf("%s：响应 = %q，应包含原始错误", target, got)
		}
	}

	e := newEnv(t)
	e.svc.createErr = errFakeRevert
	resp := e.request(t, http.MethodPost, "/api/cabins", `{"preset":"codex"}`, fakeToken, "", "")
	if got := body(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(got, errFakeRevert.Error()) {
		t.Errorf("新建失败：状态码 = %d，响应 = %q；want 400 和原始错误", resp.StatusCode, got)
	}
}

// 带请求体的路由收到坏 JSON 时返回 400，不调用 cabin.Service。
func TestCabinBadJSON(t *testing.T) {
	for _, target := range []string{
		"/api/cabins",
		"/api/cabins/c1/revert",
		"/api/cabins/c1/apply",
		"/api/holds/h1",
		"/api/cabins/c1/reset-decision",
		"/api/project-languages",
	} {
		e := newEnv(t)
		resp := e.request(t, http.MethodPost, target, `{"path":`, fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s：状态码 = %d，want %d", target, resp.StatusCode, http.StatusBadRequest)
		}
		body(t, resp)
		if calls := e.svc.recorded(); len(calls) != 0 {
			t.Errorf("%s：坏 JSON 不应调用 cabin.Service，得到 %q", target, calls)
		}
	}
}
