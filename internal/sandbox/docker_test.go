package sandbox

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 本文件的测试使用真实的 Docker，只在 AIRLOCK_DOCKER_TEST=1 时运行。

const testImage = "airlock-test-box:alpine3.20-2"

// busybox-extras 提供 httpd，用于测试入站中继。
var testDockerfile = []byte("FROM alpine:3.20\nRUN apk add --no-cache tmux util-linux-misc busybox-extras\n")

func needDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("AIRLOCK_DOCKER_TEST") != "1" {
		t.Skip("设置 AIRLOCK_DOCKER_TEST=1 后才运行真实 Docker 的测试")
	}
}

func testName(t *testing.T) string {
	b := make([]byte, 4)
	rand.Read(b)
	return "itest-" + hex.EncodeToString(b)
}

// createBox 创建沙箱，测试结束时删除沙箱和缓存卷。
func createBox(t *testing.T, s Spec) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	id, err := Docker().Create(ctx, s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		Docker().Remove(ctx, id)
		Docker().RemoveCache(ctx, s.Name)
	})
	return id
}

// inBox 在沙箱内运行 sh 命令，返回输出和是否成功。
func inBox(t *testing.T, id, script string) (string, bool) {
	t.Helper()
	out, err := exec.Command("docker", "exec", id, "sh", "-c", script).CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

func fakeGate(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "fake-gate host=%s", r.Host)
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

func baseSpec(t *testing.T) Spec {
	staging := t.TempDir()
	os.WriteFile(filepath.Join(staging, "index.html"), []byte("fake-index"), 0o644)
	return Spec{
		Name:       testName(t),
		Image:      testImage,
		Dockerfile: testDockerfile,
		StagingDir: staging,
		Env:        map[string]string{"FAKE_PLACEHOLDER": "airlock-fake-placeholder"},
		Files:      map[string]string{"~/.config/fake.toml": "fake=1"},
		GatePort:   fakeGate(t),
		Volumes:    []string{"node_modules"},
	}
}

func TestDockerIsolation(t *testing.T) {
	needDocker(t)
	s := baseSpec(t)
	id := createBox(t, s)
	cases := []struct {
		name, script string
		ok           bool
	}{
		{"经过出网中继访问闸门（V-4）", "wget -q -O - -T 5 http://airlock-gate:8080/ | grep -q 'fake-gate host=airlock-gate:8080'", true},
		{"直接访问公网 IP（V-1）", "wget -q -O /dev/null -T 5 http://1.1.1.1/", false},
		{"解析外部域名（V-2）", "nslookup example.com", false},
		{"直接访问宿主机（V-3）", "wget -q -O /dev/null -T 5 http://host.docker.internal:" + strconv.Itoa(s.GatePort) + "/", false},
		{"写入根文件系统（V-8）", "touch /etc/fake-write", false},
		{"写入暂存区", "touch /workspace/fake-from-box", true},
		{"没有 Docker socket 和宿主机磁盘（V-10）", "[ ! -e /var/run/docker.sock ] && [ ! -e /mnt/c ] && [ ! -e /host_mnt ]", true},
		{"以 1000 用户运行，没有任何能力", "[ \"$(id -u)\" = 1000 ] && grep -q '^CapEff:\\s*0000000000000000' /proc/self/status", true},
		{"/tmp 可以执行文件", "printf '#!/bin/sh\\necho ok' > /tmp/x && chmod +x /tmp/x && /tmp/x", true},
		{"Env 传入沙箱", "[ \"$FAKE_PLACEHOLDER\" = airlock-fake-placeholder ]", true},
		{"Files 写入 HOME", "[ \"$(cat ~/.config/fake.toml)\" = fake=1 ] && [ \"$HOME\" = /home/agent ]", true},
		{"缓存卷可以写入", "touch /workspace/node_modules/fake-dep", true},
	}
	for _, c := range cases {
		if out, ok := inBox(t, id, c.script); ok != c.ok {
			t.Errorf("%s：成功 = %v，want %v，输出 %q", c.name, ok, c.ok, out)
		}
	}
	if _, err := os.Stat(filepath.Join(s.StagingDir, "fake-from-box")); err != nil {
		t.Error("沙箱写入暂存区的文件没有出现在宿主机上")
	}
	for _, name := range []string{id, namesOf(s.Name).gate} {
		out, _ := exec.Command("docker", "inspect", "-f", `{{index .Config.Labels "airlock"}}`, name).Output()
		if strings.TrimSpace(string(out)) != "1" {
			t.Errorf("%s 没有标签 airlock=1", name)
		}
	}
}

// 同一个工作舱再次 Create 时使用同一组缓存卷。RemoveCache 删除它们，Prune 不删除。
func TestDockerVolumes(t *testing.T) {
	needDocker(t)
	s := baseSpec(t)
	id := createBox(t, s)
	inBox(t, id, "echo fake > /workspace/node_modules/fake-dep")
	id = createBox(t, s)
	if out, _ := inBox(t, id, "cat /workspace/node_modules/fake-dep"); out != "fake" {
		t.Fatalf("第二次运行读不到缓存卷中的文件：%q", out)
	}
	ctx := context.Background()
	if err := Docker().Prune(ctx); err != nil {
		t.Fatal(err)
	}
	vol := volumeName(s.Name, "node_modules")
	if err := exec.Command("docker", "volume", "inspect", vol).Run(); err != nil {
		t.Fatal("Prune 删除了缓存卷")
	}
	if err := exec.Command("docker", "inspect", id).Run(); err == nil {
		t.Fatal("Prune 没有删除沙箱")
	}
	if err := Docker().RemoveCache(ctx, s.Name); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("docker", "volume", "inspect", vol).Run(); err == nil {
		t.Fatal("RemoveCache 没有删除缓存卷")
	}
}

// readUntil 从会话中读，直到输出中出现 want。
func readUntil(t *testing.T, r io.Reader, want string) {
	t.Helper()
	found := make(chan bool, 1)
	go func() {
		var all strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			all.Write(buf[:n])
			if strings.Contains(all.String(), want) {
				found <- true
				return
			}
			if err != nil {
				found <- false
				return
			}
		}
	}()
	select {
	case ok := <-found:
		if !ok {
			t.Fatalf("输出中没有 %q", want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("10 秒内输出中没有 %q", want)
	}
}

func TestDockerExec(t *testing.T) {
	needDocker(t)
	id := createBox(t, baseSpec(t))
	ctx := context.Background()
	sess, err := Docker().Exec(ctx, id, []string{"sh"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	io.WriteString(sess, "echo fake-$((40+2))\r")
	readUntil(t, sess, "fake-42")
	if err := sess.Resize(123, 45); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if out, _ := inBox(t, id, "tmux display -p '#{client_width}x#{client_height}'"); out != "123x45" {
		t.Errorf("Resize 后终端大小 %q，want 123x45", out)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := inBox(t, id, "tmux has-session -t agent"); !ok {
		t.Fatal("Close 之后 agent 会话结束了")
	}
	// 再次连接时进入同一个会话，能看到之前的输出。
	sess, err = Docker().Exec(ctx, id, []string{"sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if out, _ := inBox(t, id, "tmux capture-pane -p -t agent"); !strings.Contains(out, "fake-42") {
		t.Errorf("重新连接后看不到之前的输出：%q", out)
	}
	want := []string{"docker", "exec", "-it", "-e", "TERM=xterm-256color", "-e", "LANG=C.UTF-8", id, "tmux", "new-session", "-A", "-s", "agent", "sh"}
	if got := Docker().AttachCommand(id, []string{"sh"}); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("AttachCommand = %v", got)
	}
}

// 宿主机进程异常退出时，agent 不能收到 ^D。
func TestDockerExecKilled(t *testing.T) {
	needDocker(t)
	id := createBox(t, baseSpec(t))
	sess, err := Docker().Exec(context.Background(), id, []string{"sh"})
	if err != nil {
		t.Fatal(err)
	}
	sess.(*session).kill()
	time.Sleep(2 * time.Second)
	if _, ok := inBox(t, id, "tmux has-session -t agent"); !ok {
		t.Fatal("宿主机进程被强制结束后，agent 会话结束了")
	}
}

// 主进程在 Files 写入后才启动，所以它能读到配置文件。
func TestDockerLogs(t *testing.T) {
	needDocker(t)
	s := baseSpec(t)
	s.Cmd = []string{"sh", "-c", "echo fake-start $(cat ~/.config/fake.toml); exec sleep 600"}
	id := createBox(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := Docker().Logs(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "fake-start fake=1" {
		t.Fatalf("Logs 第一行 %q, %v", line, err)
	}
}

// 入站中继只映射到宿主机的 127.0.0.1。
func TestDockerWebRelay(t *testing.T) {
	needDocker(t)
	s := baseSpec(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	s.HostPort = l.Addr().(*net.TCPAddr).Port
	l.Close()
	s.WebPort = 8000
	s.Cmd = []string{"httpd", "-f", "-p", "8000", "-h", "/workspace"}
	id := createBox(t, s)
	client := &http.Client{Timeout: 3 * time.Second}
	var body string
	for range 20 {
		resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(s.HostPort) + "/index.html")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if body != "fake-index" {
		inside, _ := inBox(t, id, "wget -q -O - -T 3 http://127.0.0.1:8000/index.html")
		relayLog, _ := exec.Command("docker", "logs", namesOf(s.Name).web).CombinedOutput()
		boxLog, _ := exec.Command("docker", "logs", id).CombinedOutput()
		t.Fatalf("经过入站中继读到 %q。沙箱内直接访问：%q。中继日志：%q。沙箱日志：%q", body, inside, relayLog, boxLog)
	}
	out, _ := exec.Command("docker", "port", namesOf(s.Name).web).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.Contains(line, "127.0.0.1:"+strconv.Itoa(s.HostPort)) {
			t.Errorf("入站中继映射到了 127.0.0.1 以外：%q", line)
		}
	}
}
