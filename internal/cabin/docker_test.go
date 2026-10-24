package cabin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

// 本文件的测试使用真实的 Docker 和预设镜像，只在 AIRLOCK_DOCKER_TEST=1 时运行。
// 闸门读取不到真实密钥，所以模型请求不会带着密钥离开本机。

func TestDockerRun(t *testing.T) {
	if os.Getenv("AIRLOCK_DOCKER_TEST") != "1" {
		t.Skip("设置 AIRLOCK_DOCKER_TEST=1 后才运行真实 Docker 的测试")
	}
	for _, preset := range []string{"claude-code", "deepseek-harness"} {
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
			if preset == "claude-code" {
				u, _ := url.Parse(raw)
				s, err := m.Attach(ctx, c.ID, strings.TrimPrefix(u.Fragment, "token="))
				if err != nil {
					t.Fatal(err)
				}
				if out := readUntil(s, "Claude", time.Minute); !strings.Contains(out, "Claude") {
					t.Fatalf("终端中没有出现 Claude Code：%q", out)
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

// readUntil 读取终端输出，直到出现 want 或超时。
func readUntil(s sandbox.Session, want string, d time.Duration) string {
	var mu sync.Mutex
	var out strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := out.String()
		mu.Unlock()
		if strings.Contains(got, want) {
			return got
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	return out.String()
}
