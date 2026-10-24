package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Spec.Name 只能含小写字母、数字和连字符。其他名称必须在调用 docker 前被拒绝。
func TestCreateRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "Bad", "a b", "a;rm -rf /", "../x"} {
		if id, err := Docker().Create(context.Background(), Spec{Name: name}); err == nil || id != "" {
			t.Errorf("Create(%q) = %q, %v; want error", name, id, err)
		}
	}
}

// RemoveCache 的 name 会拼进卷名。非法名称必须在调用 docker 前被拒绝。
func TestRemoveCacheRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "Bad", "a b", "a;rm -rf /", "../x"} {
		if err := Docker().RemoveCache(context.Background(), name); err == nil {
			t.Errorf("RemoveCache(%q) 没有报错", name)
		}
	}
}

// fakeDocker 记录 docker 命令，按 reply 返回结果，不调用真实的 docker。
type fakeDocker struct {
	mu     sync.Mutex
	calls  [][]string
	stdins map[int][]byte
	envs   map[int]string // create 调用时 env 文件的内容
	reply  func(args []string) ([]byte, error)
}

func (f *fakeDocker) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := len(f.calls)
	f.calls = append(f.calls, args)
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		if f.stdins == nil {
			f.stdins = map[int][]byte{}
		}
		f.stdins[i] = b
	}
	if args[0] == "create" {
		if j := slices.Index(args, "--env-file"); j >= 0 {
			b, _ := os.ReadFile(args[j+1])
			if f.envs == nil {
				f.envs = map[int]string{}
			}
			f.envs[i] = string(b)
		}
	}
	if f.reply != nil {
		return f.reply(args)
	}
	return nil, nil
}

func (f *fakeDocker) backend() docker { return docker{run: f.run, start: helperStart} }

// call 返回第一条以 prefix 开头的命令。
func (f *fakeDocker) call(t *testing.T, prefix ...string) (int, []string) {
	t.Helper()
	for i, c := range f.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return i, c
		}
	}
	t.Fatalf("没有执行 docker %v，全部命令：%v", prefix, f.calls)
	return 0, nil
}

func (f *fakeDocker) has(prefix ...string) bool {
	for _, c := range f.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// newVolumes 让 volume inspect 失败，模拟卷还不存在。
func newVolumes(args []string) ([]byte, error) {
	if len(args) > 1 && args[0] == "volume" && args[1] == "inspect" {
		return nil, errors.New("fake: no such volume")
	}
	return nil, nil
}

func goodSpec() Spec {
	return Spec{
		Name:       "fake-cabin",
		Image:      "airlock-claude-code:1",
		Dockerfile: []byte("FROM fake"),
		StagingDir: absDir(),
		Env:        map[string]string{"ANTHROPIC_API_KEY": "airlock-fake-placeholder", "ANTHROPIC_BASE_URL": "http://airlock-gate:8080/anthropic"},
		Files:      map[string]string{"~/.codex/config.toml": "fake-config-content", "/tmp/fake.txt": "fake-tmp"},
		Cmd:        []string{"claude"},
		GatePort:   43210,
		Volumes:    []string{"node_modules", ".venv"},
	}
}

func absDir() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "/fake"
}
func TestVolumeName(t *testing.T) {
	a, b := volumeName("fake-cabin", "node_modules"), volumeName("fake-cabin", "node_modules")
	if a != b || !strings.HasPrefix(a, "airlock-cache-fake-cabin-") {
		t.Errorf("卷名不稳定：%s %s", a, b)
	}
	if a == volumeName("fake-cabin", ".venv") || a == volumeName("fake-other", "node_modules") {
		t.Error("不同的目录或工作舱得到同一个卷名")
	}
}

func TestNameOfID(t *testing.T) {
	if name, err := nameOfID("airlock-fake-cabin-box"); err != nil || name != "fake-cabin" {
		t.Errorf("nameOfID = %q, %v", name, err)
	}
	for _, id := range []string{"", "fake", "airlock-fake-cabin-gate", "airlock--box", "airlock-Bad-box", "other-fake-box", "airlock-a;b-box"} {
		if _, err := nameOfID(id); err == nil {
			t.Errorf("nameOfID(%q) 没有报错", id)
		}
	}
}

// TestHelperDocker 不是真正的测试。helperStart 把测试程序本身当作假的 docker 命令运行。
func TestHelperDocker(t *testing.T) {
	mode := os.Getenv("AIRLOCK_FAKE_DOCKER")
	if mode == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	switch {
	case args[0] == "logs":
		fmt.Println("fake-log-line")
		time.Sleep(time.Minute)
	case mode == "bad-tty":
		fmt.Println("AIRLOCK-TTY /dev/pts/0; rm -rf /")
	case mode == "silent":
		time.Sleep(time.Minute)
	case mode == "stuck":
		fmt.Print("AIRLOCK-TTY /dev/pts/7\r\n")
		time.Sleep(time.Minute)
	default:
		fmt.Print("AIRLOCK-TTY /dev/pts/7\r\n")
		io.Copy(os.Stdout, os.Stdin)
	}
	os.Exit(0)
}

var helperMode = "echo"

func helperStart(args ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperDocker$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "AIRLOCK_FAKE_DOCKER="+helperMode)
	return cmd
}

func setHelperMode(t *testing.T, mode string) {
	helperMode = mode
	t.Cleanup(func() { helperMode = "echo" })
}

func TestFilePath(t *testing.T) {
	for in, want := range map[string]string{"~/.codex/config.toml": "/home/agent/.codex/config.toml", "/tmp/x": "/tmp/x"} {
		if got, err := filePath(in); err != nil || got != want {
			t.Errorf("filePath(%q) = %q, %v", in, got, err)
		}
	}
}
