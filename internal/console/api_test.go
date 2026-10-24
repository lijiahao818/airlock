package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/presets"
)

// AC-1：设置类路由调用对应的方法，返回对应的数据。
func TestSettingsRoutes(t *testing.T) {
	t.Run("docker", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, "/api/docker", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusOK)
		}
		if got := body(t, resp); strings.TrimSpace(got) != "{}" {
			t.Errorf("响应 = %q，want {}", got)
		}
		if got := e.svc.recorded(); strings.Join(got, ";") != "CheckDocker" {
			t.Errorf("调用记录 = %q", got)
		}
	})

	t.Run("docker 没有就绪", func(t *testing.T) {
		e := newEnv(t)
		e.svc.checkDockerErr = errFakeDocker
		resp := e.request(t, http.MethodGet, "/api/docker", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusServiceUnavailable)
		}
		if got := body(t, resp); !strings.Contains(got, "Docker 没有就绪") {
			t.Errorf("响应 = %q，应说明 Docker 没有就绪", got)
		}
	})

	t.Run("providers 带密钥提示", func(t *testing.T) {
		e := newEnv(t)
		e.keys.hints["anthropic"] = "ab12"
		resp := e.request(t, http.MethodGet, "/api/providers", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		want := `[{"name":"anthropic","hint":"ab12"},{"name":"openai","hint":""},{"name":"deepseek","hint":""}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
		if got := e.keys.recorded(); strings.Join(got, ";") != `Hint "anthropic";Hint "openai";Hint "deepseek"` {
			t.Errorf("调用记录 = %q", got)
		}
	})

	t.Run("providers 没有密钥", func(t *testing.T) {
		e := newEnv(t)
		e.keys.hintErr = errFakeHint
		resp := e.request(t, http.MethodGet, "/api/providers", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		want := `[{"name":"anthropic","hint":""},{"name":"openai","hint":""},{"name":"deepseek","hint":""}]`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("保存和删除密钥", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodPut, "/api/keys/anthropic", `{"secret":"fake-secret-1234567890"}`, fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		body(t, resp)
		if got := e.keys.recorded(); strings.Join(got, ";") != `Set "anthropic"` {
			t.Errorf("调用记录 = %q", got)
		}
		if got := e.keys.secretOf("anthropic"); got != "fake-secret-1234567890" {
			t.Errorf("Set 收到的密钥 = %q", got)
		}

		resp = e.request(t, http.MethodDelete, "/api/keys/openai", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("删除：状态码 = %d", resp.StatusCode)
		}
		body(t, resp)
		if got := e.keys.recorded(); got[len(got)-1] != `Delete "openai"` {
			t.Errorf("调用记录 = %q", got)
		}
	})

	t.Run("保存空的密钥被拒绝", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodPut, "/api/keys/anthropic", `{"secret":""}`, fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
		}
		if got := body(t, resp); !strings.Contains(got, "粘贴完整") {
			t.Errorf("响应 = %q，应说明下一步", got)
		}
		if got := e.keys.recorded(); len(got) != 0 {
			t.Errorf("空的密钥不应该调用 Set：%q", got)
		}
	})

	t.Run("测试密钥", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodPost, "/api/keys/deepseek/test", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		body(t, resp)

		e.keys.err = errFakeKey
		resp = e.request(t, http.MethodPost, "/api/keys/deepseek/test", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("失败：状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
		}
		if got := body(t, resp); !strings.Contains(got, errFakeKey.Error()) {
			t.Errorf("响应 = %q，应包含原始错误", got)
		}
	})

	t.Run("预设", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, "/api/presets", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		var all []presets.Preset
		if err := json.Unmarshal([]byte(body(t, resp)), &all); err != nil {
			t.Fatalf("解析预设：%v", err)
		}
		if len(all) == 0 {
			t.Error("预设列表是空的")
		}
	})

	t.Run("档位", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodGet, "/api/tiers", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		if got := strings.TrimSpace(body(t, resp)); got != `["strict","standard","permissive"]` {
			t.Errorf("响应 = %s", got)
		}
	})

	t.Run("读取副驾设置", func(t *testing.T) {
		e := newEnv(t)
		e.adv.settings = advisor.Settings{Enabled: true, Provider: "anthropic", NoticeShown: true}
		resp := e.request(t, http.MethodGet, "/api/advisor", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		want := `{"enabled":true,"provider":"anthropic","noticeShown":true}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("保存副驾设置", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodPut, "/api/advisor", `{"enabled":true,"provider":"deepseek","noticeShown":true}`, fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		body(t, resp)
		if got := e.adv.received(); got != (advisor.Settings{Enabled: true, Provider: "deepseek", NoticeShown: true}) {
			t.Errorf("收到的设置 = %+v", got)
		}

		e.adv.err = errFakeAdvisor
		resp = e.request(t, http.MethodPut, "/api/advisor", `{}`, fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("失败：状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
		}
		body(t, resp)
	})

	t.Run("选择项目文件夹", func(t *testing.T) {
		e := newEnv(t)
		resp := e.request(t, http.MethodPost, "/api/pick-folder", "", fakeToken, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d", resp.StatusCode)
		}
		want := `{"path":"D:\\fake-project"}`
		if got := strings.TrimSpace(body(t, resp)); got != want {
			t.Errorf("响应 = %s，want %s", got, want)
		}
	})

	t.Run("未知的接口", func(t *testing.T) {
		e := newEnv(t)
		for name, method := range map[string]string{
			"未知路径":   http.MethodGet,
			"未知方法":   http.MethodPatch,
			"预检请求":   http.MethodOptions,
			"不常见的方法": "PROPFIND",
		} {
			resp := e.request(t, method, "/api/no-such-api", "", fakeToken, "", "")
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s（%s）：状态码 = %d，want %d", name, method, resp.StatusCode, http.StatusNotFound)
			}
			if got := body(t, resp); !strings.Contains(got, "没有这个接口") {
				t.Errorf("%s（%s）：响应 = %q", name, method, got)
			}
		}
	})
}

// AC-7：请求体有大小上限，JSON 格式错误时返回 400，错误写明下一步操作。
func TestBodyLimitAndJSON(t *testing.T) {
	e := newEnv(t)
	// 65 KiB 的空白之后才是合法的 JSON，解析时先读到超限。
	big := strings.Repeat(" ", 65<<10) + "{}"
	resp := e.request(t, http.MethodPut, "/api/advisor", big, fakeToken, "", "")
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("超限：状态码 = %d，want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if got := body(t, resp); !strings.Contains(got, "太大") || !strings.Contains(got, "重试") {
		t.Errorf("超限响应 = %q，应说明下一步", got)
	}

	for name, target := range map[string]string{
		"密钥请求":   "/api/keys/anthropic",
		"副驾设置请求": "/api/advisor",
	} {
		resp := e.request(t, http.MethodPut, target, `{"secret":`, fakeToken, "", "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s：状态码 = %d，want %d", name, resp.StatusCode, http.StatusBadRequest)
		}
		if got := body(t, resp); !strings.Contains(got, "JSON") || !strings.Contains(got, "重试") {
			t.Errorf("%s：响应 = %q，应说明下一步", name, got)
		}
	}
}

// AC-8：保存密钥的请求中，密钥不出现在响应里。
func TestSecretNeverLeaked(t *testing.T) {
	const secret = "fake-secret-1234567890"
	cases := []struct {
		name   string
		body   string
		setErr bool
	}{
		{"保存成功", `{"secret":"` + secret + `"}`, false},
		{"保存失败", `{"secret":"` + secret + `"}`, true},
		{"JSON 格式错误", `{"secret":"` + secret + `"`, false},
	}
	for _, tt := range cases {
		e := newEnv(t)
		if tt.setErr {
			e.keys.err = errFakeKey
		}
		resp := e.request(t, http.MethodPut, "/api/keys/anthropic", tt.body, fakeToken, "", "")
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s：状态码 = %d", tt.name, resp.StatusCode)
		}
		if got := body(t, resp); strings.Contains(got, secret) {
			t.Errorf("%s：响应泄漏了密钥：%q", tt.name, got)
		}
	}

	// 下层的错误文本里带密钥时，换成固定的中文提示。
	e := newEnv(t)
	e.keys.err = errors.New("fake: 保存 fake-secret-1234567890 失败")
	resp := e.request(t, http.MethodPut, "/api/keys/anthropic", `{"secret":"fake-secret-1234567890"}`, fakeToken, "", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("状态码 = %d，want %d", resp.StatusCode, http.StatusBadRequest)
	}
	got := body(t, resp)
	if strings.Contains(got, "fake-secret-1234567890") {
		t.Errorf("响应泄漏了密钥：%q", got)
	}
	if !strings.Contains(got, "已隐藏") {
		t.Errorf("响应 = %q，应说明密钥被隐藏", got)
	}
}
