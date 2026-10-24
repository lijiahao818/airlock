package presets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func TestClaudeCodeSampleMatchesPreset(t *testing.T) {
	data, err := os.ReadFile("claude-code/preset.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Preset
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("preset.json 不符合 Preset：%v", err)
	}
	want := Preset{
		ID:       "claude-code",
		Name:     "Claude Code",
		Provider: "anthropic",
		UI:       "terminal",
		Cmd:      []string{"claude"},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                                   "{gate}/anthropic",
			"ANTHROPIC_API_KEY":                                    "{placeholder}",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":             "1",
			"CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL": "1",
			"HTTPS_PROXY":                                          "{gate}",
		},
		Files: map[string]string{"~/.claude.json": "{\"hasCompletedOnboarding\": true, \"customApiKeyResponses\": {\"approved\": [\"{placeholder-tail}\"], \"rejected\": []}}\n"},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got %+v\nwant %+v", p, want)
	}
}

func TestAllReturnsThreePresetsSortedByID(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range all {
		ids = append(ids, p.ID)
	}
	if want := []string{"claude-code", "codex", "deepseek-harness"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

func TestGet(t *testing.T) {
	p, err := Get("claude-code")
	if err != nil || p.Provider != "anthropic" {
		t.Fatalf("Get(claude-code) = %+v, %v", p, err)
	}
	if _, err := Get("no-such-agent"); err == nil {
		t.Fatal("未知 ID 应该返回错误")
	}
}

func TestDockerfileMatchesFile(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		want, err := os.ReadFile(p.ID + "/Dockerfile")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(p.Dockerfile, want) {
			t.Errorf("%s: Dockerfile 字段与文件不同", p.ID)
		}
	}
}

// 镜像必须基于 glibc 的发行版，以非 root 用户运行。terminal 型要装 tmux，script 来自 Debian 自带的 bsdutils。
func TestDockerfileRequirements(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		d := string(p.Dockerfile)
		if !strings.HasPrefix(d, "FROM node:") || !strings.Contains(d, "-bookworm") {
			t.Errorf("%s: 镜像必须基于 Debian bookworm", p.ID)
		}
		// 标签可以被重新推送，所以基础镜像按 digest 锁定。
		if !regexp.MustCompile(`\AFROM \S+@sha256:[0-9a-f]{64}\n`).MatchString(d) {
			t.Errorf("%s: 基础镜像必须按 digest 锁定", p.ID)
		}
		if !strings.Contains(d, "\nUSER node\n") {
			t.Errorf("%s: 镜像必须以非 root 用户运行", p.ID)
		}
		if p.UI == "terminal" && !strings.Contains(d, " tmux") {
			t.Errorf("%s: terminal 型镜像必须安装 tmux", p.ID)
		}
		for _, m := range regexp.MustCompile(`npm install -g (\S+)`).FindAllStringSubmatch(d, -1) {
			if !regexp.MustCompile(`^@[a-z0-9-]+/[a-z0-9-]+@[0-9][0-9A-Za-z.-]*$`).MatchString(m[1]) {
				t.Errorf("%s: %s 必须锁定版本", p.ID, m[1])
			}
		}
	}
}

// 密钥类的值只能是占位符，配置文件中不能出现密钥。
func TestNoSecretsInPresets(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	secretName := regexp.MustCompile(`(?i)key|token|secret|password`)
	for _, p := range all {
		for k, v := range p.Env {
			if secretName.MatchString(k) && v != "{placeholder}" {
				t.Errorf("%s: %s 必须是 {placeholder}", p.ID, k)
			}
		}
		for path, content := range p.Files {
			if strings.Contains(content, "{placeholder}") || regexp.MustCompile(`(?i)sk-|api_key\s*=`).MatchString(content) {
				t.Errorf("%s: %s 中不能写密钥，只能写环境变量名", p.ID, path)
			}
		}
	}
}

func TestCodexConfig(t *testing.T) {
	p, err := Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	c := p.Files["~/.codex/config.toml"]
	for _, want := range []string{
		`model_provider = "airlock"`,
		`base_url = "{gate}/openai/v1"`,
		`env_key = "OPENAI_API_KEY"`,
		"supports_websockets = false",
		"[analytics]\nenabled = false",
		`metrics_exporter = "none"`,
		// 关闭启动时对 chatgpt.com、github.com、api.github.com 的插件同步请求。
		"[features]\nplugins = false",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("config.toml 缺少 %q", want)
		}
	}
	if p.Env["OPENAI_API_KEY"] != "{placeholder}" {
		t.Error("OPENAI_API_KEY 必须是占位符")
	}
	// /home/agent 不允许执行文件，后台程序无法运行。
	if !reflect.DeepEqual(p.Cmd, []string{"codex", "--no-daemon"}) {
		t.Errorf("Cmd = %q", p.Cmd)
	}
}

func TestDeepseekHarness(t *testing.T) {
	p, err := Get("deepseek-harness")
	if err != nil {
		t.Fatal(err)
	}
	if p.UI != "web" || p.Web == nil {
		t.Fatalf("got %+v", p)
	}
	for k, want := range map[string]string{"DEEPSEEK_BASE_URL": "{gate}/deepseek", "DEEPSEEK_API_KEY": "{placeholder}", "HTTPS_PROXY": "{gate}"} {
		if p.Env[k] != want {
			t.Errorf("%s = %q, want %q", k, p.Env[k], want)
		}
	}
	if !strings.Contains(strings.Join(p.Cmd, " "), "--trusted-host {webhost}") {
		t.Errorf("启动参数缺少 --trusted-host {webhost}：%q", p.Cmd)
	}
	// 入站中继转发到 web.port，dsh 必须监听同一个端口。两者不同时，网页界面打不开。
	if ports := regexp.MustCompile(`--port (\d+)`).FindAllStringSubmatch(strings.Join(p.Cmd, " "), -1); len(ports) != 1 || ports[0][1] != fmt.Sprint(p.Web.Port) {
		t.Errorf("启动参数中的 --port 必须只有一个，并且等于 web.port %d：%q", p.Web.Port, p.Cmd)
	}
	// 输出行来自团队在 dsh 0.2.1-rc.2 上的实测，令牌为假值。
	re := regexp.MustCompile(p.Web.TokenPattern)
	line := "dsh web: http://172.22.0.2:3080/?token=fake_abcdefghijklmnopqrstuvwxyz0123456789-_"
	if m := re.FindStringSubmatch(line); m == nil || m[1] != "fake_abcdefghijklmnopqrstuvwxyz0123456789-_" {
		t.Errorf("tokenPattern 提取失败：%q", m)
	}
	for _, bad := range []string{
		"dsh web: http://172.22.0.2:3080/?token=short",
		"dsh web: http://172.22.0.2:3080/?token=fake_abcdefghijklmnopqrstuvwxyz0123456789-_\"><script>",
		"evil: http://x/?token=fake_abcdefghijklmnopqrstuvwxyz0123456789-_",
	} {
		if re.MatchString(bad) {
			t.Errorf("tokenPattern 不应该匹配 %q", bad)
		}
	}
}

func TestLoadRejectsBadPresets(t *testing.T) {
	const dockerfile = "FROM node:24.22.1-bookworm-slim\nUSER node\n"
	web := `"ui": "web", "web": {"port": 3080, "openPath": "/?token={token}", "tokenPattern": %q}`
	cases := map[string]string{
		"缺少 web":           `"ui": "web"`,
		"没有分组":             fmt.Sprintf(web, `token=[a-z]+`),
		"两个分组":             fmt.Sprintf(web, `(token)=([a-z]+)`),
		"正则不能编译":           fmt.Sprintf(web, `token=([a-z]+`),
		"端口为 0":            `"ui": "web", "web": {"port": 0, "openPath": "/?token={token}", "tokenPattern": "(x)"}`,
		"terminal 型带 web":  `"ui": "terminal", "web": {"port": 3080, "openPath": "/?token={token}", "tokenPattern": "(x)"}`,
		"openPath 不以 / 开头": `"ui": "web", "web": {"port": 3080, "openPath": "@evil.example/?token={token}", "tokenPattern": "(x)"}`,
		"openPath 不含令牌":    `"ui": "web", "web": {"port": 3080, "openPath": "/", "tokenPattern": "(x)"}`,
		"未知界面类型":           `"ui": "gui"`,
		"未知供应商":            `"ui": "terminal", "provider": "evil"`,
		"未知字段":             `"ui": "terminal", "apiKey": "fake-key"`,
		"id 与目录名不同":        `"ui": "terminal", "id": "other"`,
		"cmd 为空":           `"ui": "terminal", "cmd": []`,
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			// 后出现的同名字段覆盖前面的默认值。
			j := `{"id": "x", "name": "X", "provider": "anthropic", "cmd": ["x"], ` + fields + `}`
			fsys := fstest.MapFS{"x/preset.json": {Data: []byte(j)}, "x/Dockerfile": {Data: []byte(dockerfile)}}
			if _, err := load(fsys); err == nil {
				t.Fatalf("应该拒绝：%s", j)
			}
		})
	}
	t.Run("缺少 Dockerfile", func(t *testing.T) {
		fsys := fstest.MapFS{"x/preset.json": {Data: []byte(`{"id": "x", "name": "X", "provider": "anthropic", "ui": "terminal", "cmd": ["x"]}`)}}
		if _, err := load(fsys); err == nil {
			t.Fatal("应该拒绝没有 Dockerfile 的预设")
		}
	})
	t.Run("缺少 preset.json", func(t *testing.T) {
		fsys := fstest.MapFS{"x/Dockerfile": {Data: []byte(dockerfile)}}
		if _, err := load(fsys); err == nil {
			t.Fatal("应该拒绝没有 preset.json 的目录")
		}
	})
	t.Run("name 为空", func(t *testing.T) {
		fsys := fstest.MapFS{"x/preset.json": {Data: []byte(`{"id": "x", "provider": "anthropic", "ui": "terminal", "cmd": ["x"]}`)}, "x/Dockerfile": {Data: []byte(dockerfile)}}
		if _, err := load(fsys); err == nil {
			t.Fatal("应该拒绝没有 name 的预设")
		}
	})
	t.Run("Dockerfile 为空", func(t *testing.T) {
		fsys := fstest.MapFS{"x/preset.json": {Data: []byte(`{"id": "x", "name": "X", "provider": "anthropic", "ui": "terminal", "cmd": ["x"]}`)}, "x/Dockerfile": {Data: nil}}
		if _, err := load(fsys); err == nil {
			t.Fatal("应该拒绝空的 Dockerfile")
		}
	})
	t.Run("合法的 web 预设", func(t *testing.T) {
		j := `{"id": "x", "name": "X", "provider": "anthropic", "cmd": ["x"], ` + fmt.Sprintf(web, `token=([a-z]+)`) + `}`
		fsys := fstest.MapFS{"x/preset.json": {Data: []byte(j)}, "x/Dockerfile": {Data: []byte(dockerfile)}}
		if _, err := load(fsys); err != nil {
			t.Fatal(err)
		}
	})
}

// 根目录下的普通文件不是预设，加载时跳过。
func TestLoadSkipsFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md":     {Data: []byte("不是预设")},
		"x/preset.json": {Data: []byte(`{"id": "x", "name": "X", "provider": "anthropic", "ui": "terminal", "cmd": ["x"]}`)},
		"x/Dockerfile":  {Data: []byte("FROM node:24.22.1-bookworm-slim\nUSER node\n")},
	}
	all, err := load(fsys)
	if err != nil || len(all) != 1 || all[0].ID != "x" {
		t.Fatalf("load = %+v, %v", all, err)
	}
	if p, err := get(fsys, "x"); err != nil || p.Name != "X" {
		t.Fatalf("get = %+v, %v", p, err)
	}
}

// 预设目录不能读取时，load 和 get 都返回错误。
func TestLoadReportsUnreadableDir(t *testing.T) {
	fsys := os.DirFS(t.TempDir() + "/no-such-dir")
	if _, err := load(fsys); err == nil {
		t.Fatal("load 应该返回错误")
	}
	if _, err := get(fsys, "claude-code"); err == nil {
		t.Fatal("get 应该返回错误")
	}
}
