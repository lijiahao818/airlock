package cabin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/presets"
)

// 本文件的测试使用真实的 Docker 和预设镜像，只在 AIRLOCK_DOCKER_TEST=1 时运行。
// 闸门读取不到真实密钥，所以模型请求不会带着密钥离开本机。

func TestDockerRun(t *testing.T) {
	if os.Getenv("AIRLOCK_DOCKER_TEST") != "1" {
		t.Skip("设置 AIRLOCK_DOCKER_TEST=1 后才运行真实 Docker 的测试")
	}
	for _, preset := range []string{"claude-code", "codex", "deepseek-harness"} {
		t.Run(preset, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			m, err := New(Options{
				DataDir: t.TempDir(), Backend: sandbox.Docker(), TermURL: "http://127.0.0.1:1",
				Secret: func(string) (string, error) { return "", errors.New("fake: 测试中没有密钥") },
			})
			if err != nil {
				t.Fatal(err)
			}
			c, err := m.Create(newProject(t), preset, "strict")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Start(ctx, c.ID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Reset(context.Background(), c.ID) })
			box := "airlock-" + c.ID + "-box"

			raw, _ := m.AgentURL(c.ID)
			if ui, ok := terminalUI[preset]; ok {
				u, _ := url.Parse(raw)
				s, err := m.Attach(ctx, c.ID, strings.TrimPrefix(u.Fragment, "token="))
				if err != nil {
					t.Fatal(err)
				}
				go io.Copy(io.Discard, s)
				if got := waitScreen(ctx, box, ui.trust, time.Minute); !strings.Contains(got, ui.trust) {
					t.Fatalf("终端中没有出现信任文件夹的询问：\n%s", got)
				}
				for _, k := range ui.keys {
					if _, err := io.WriteString(s, k); err != nil {
						t.Fatal(err)
					}
					time.Sleep(500 * time.Millisecond) // 分开发送，agent 才能识别为两次按键
				}
				if got := waitScreen(ctx, box, ui.ui, time.Minute); !strings.Contains(got, ui.ui) {
					t.Fatalf("终端中没有出现 agent 的交互界面：\n%s", got)
				}
				// agent 启动后立即退出时，界面可能已经出现过。连接 60 秒后再检查进程和屏幕。
				time.Sleep(60 * time.Second)
				p, err := presets.Get(preset)
				if err != nil {
					t.Fatal(err)
				}
				if !running(ctx, box, p.Cmd[0]) {
					t.Fatalf("连接 60 秒后 %s 进程已经退出", p.Cmd[0])
				}
				if got := screen(ctx, box); !strings.Contains(got, ui.ui) {
					t.Fatalf("连接 60 秒后终端中没有 agent 的界面：\n%s", got)
				}
				s.Close()
			} else {
				// 不跟随重定向，才能看到令牌换来的 cookie。
				client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				resp, err := client.Get(raw)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) == 0 {
					t.Fatalf("带令牌访问 %s：%d，cookie %d 个", strings.Split(raw, "?")[0], resp.StatusCode, len(resp.Cookies()))
				}
			}

			// 模拟 agent 在 /workspace 中新建一个文件。
			if out, err := exec.CommandContext(ctx, "docker", "exec", box, "sh", "-c", "echo agent > /workspace/from-agent.txt").CombinedOutput(); err != nil {
				t.Fatalf("写入文件失败：%v %s", err, out)
			}
			if err := m.Stop(ctx, c.ID); err != nil {
				t.Fatal(err)
			}
			items, err := m.ReviewItems(c.ID)
			if err != nil || len(items) != 1 || items[0].Path != "from-agent.txt" {
				t.Fatalf("ReviewItems = %+v, %v", items, err)
			}
			if err := m.Apply(c.ID, "from-agent.txt"); err != nil {
				t.Fatal(err)
			}
			if err := m.FinishReview(c.ID); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(filepath.Join(c.Project, "from-agent.txt")); string(data) != "agent\n" {
				t.Fatalf("项目中的文件 = %q", data)
			}
		})
	}
}

// terminalUI 描述 terminal 型 agent 首次启动时的界面。两个 agent 都先询问是否信任 /workspace。
// trust 是询问时屏幕上的文字，keys 选择信任，ui 是之后的交互界面上的文字。
// Claude Code 没有询问是否使用 API key，说明预设预先批准了占位符。
var terminalUI = map[string]struct {
	trust string
	keys  []string
	ui    string
}{
	"claude-code": {"trust this folder", []string{"\x1b[B", "\r"}, "shift+tab to cycle"},
	"codex":       {"Trust this folder?", []string{"\r"}, "for shortcuts"},
}

// screen 返回沙箱内 agent 所在的 tmux 窗格当前显示的文字。终端输出中有光标控制序列，不能直接查找文字。
func screen(ctx context.Context, box string) string {
	out, _ := exec.CommandContext(ctx, "docker", "exec", box, "tmux", "capture-pane", "-p", "-t", "agent").Output()
	return string(out)
}

// waitScreen 等待屏幕上出现 want，超时时返回最后的屏幕。
func waitScreen(ctx context.Context, box, want string, d time.Duration) string {
	deadline := time.Now().Add(d)
	for {
		got := screen(ctx, box)
		if strings.Contains(got, want) || time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Second)
	}
}

// running 检查沙箱内有没有名为 name 的进程。comm 最多保存进程名的前 15 个字节。
func running(ctx context.Context, box, name string) bool {
	out, _ := exec.CommandContext(ctx, "docker", "exec", box, "sh", "-c", "cat /proc/[0-9]*/comm").Output()
	want := name[:min(len(name), 15)]
	for _, line := range strings.Split(string(out), "\n") {
		if line == want {
			return true
		}
	}
	return false
}
