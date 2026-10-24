package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCreateBoxArgs(t *testing.T) {
	f := &fakeDocker{reply: newVolumes}
	s := goodSpec()
	id, err := f.backend().Create(context.Background(), s)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "airlock-fake-cabin-box" {
		t.Errorf("id = %q", id)
	}
	i, args := f.call(t, "create")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--user 1000:1000", "--cap-drop ALL", "--security-opt no-new-privileges", "--read-only",
		"--pids-limit 512", "--memory 4g", "--network airlock-fake-cabin-in", "--label airlock=1",
		"--mount type=bind,src=" + s.StagingDir + ",dst=/workspace",
		"--mount type=volume,src=" + volumeName(s.Name, "node_modules") + ",dst=/workspace/node_modules",
		"--mount type=volume,src=" + volumeName(s.Name, ".venv") + ",dst=/workspace/.venv",
		"--tmpfs /tmp:rw,exec", "--env HOME=/home/agent", "--pull never",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("沙箱参数中没有 %q：%s", want, joined)
		}
	}
	// 只挂载暂存区和缓存卷。
	if n := strings.Count(joined, "--mount "); n != 3 {
		t.Errorf("挂载了 %d 个目录，want 3", n)
	}
	home, _ := os.UserHomeDir()
	for _, bad := range []string{"--privileged", "docker.sock", "SSH_AUTH_SOCK", " -v ", "--volume", "airlock-fake-placeholder", "fake-config-content"} {
		if strings.Contains(joined, bad) {
			t.Errorf("沙箱参数中有 %q：%s", bad, joined)
		}
	}
	if home != "" && strings.Contains(joined, "src="+home+",") {
		t.Errorf("挂载了宿主机 home 目录：%s", joined)
	}
	// Env 通过 env 文件传入，创建后删除这个文件。
	if env := f.envs[i]; env != "ANTHROPIC_API_KEY=airlock-fake-placeholder\nANTHROPIC_BASE_URL=http://airlock-gate:8080/anthropic\n" {
		t.Errorf("env 文件内容 %q", env)
	}
	if _, err := os.Stat(args[slices.Index(args, "--env-file")+1]); !os.IsNotExist(err) {
		t.Error("创建沙箱后 env 文件没有删除")
	}
	// 主进程先等 Files 写入，再运行 Cmd。
	if tail := args[len(args)-7:]; !slices.Equal(tail, []string{"--entrypoint", "sh", s.Image, "-c", waitScript, "sh", "claude"}) {
		t.Errorf("主进程 %v", tail)
	}
	for _, c := range f.calls {
		for _, a := range c {
			if strings.Contains(a, "airlock-fake-placeholder") || strings.Contains(a, "fake-config-content") {
				t.Errorf("docker 参数中有 Env 或 Files 的内容：%v", c)
			}
		}
	}
}

func TestCreateWritesFilesBeforeCmd(t *testing.T) {
	f := &fakeDocker{reply: newVolumes}
	if _, err := f.backend().Create(context.Background(), goodSpec()); err != nil {
		t.Fatal(err)
	}
	start, _ := f.call(t, "start")
	i, args := f.call(t, "exec", "-i", "airlock-fake-cabin-box", "tar")
	if i < start {
		t.Error("Files 在沙箱启动前写入")
	}
	if !slices.Equal(args, []string{"exec", "-i", "airlock-fake-cabin-box", "tar", "-x", "-C", "/", "-f", "-"}) {
		t.Errorf("写入 Files 的命令 %v", args)
	}
	got := map[string]string{}
	var order []string
	r := tar.NewReader(bytes.NewReader(f.stdins[i]))
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		b, _ := io.ReadAll(r)
		got[h.Name] = string(b)
		order = append(order, h.Name)
		if h.Uid != 1000 || h.Mode != 0o600 {
			t.Errorf("%s 的属主或权限 %d %o", h.Name, h.Uid, h.Mode)
		}
	}
	if got["home/agent/.codex/config.toml"] != "fake-config-content" || got["tmp/fake.txt"] != "fake-tmp" {
		t.Errorf("tar 内容 %v", got)
	}
	if order[len(order)-1] != "tmp/.airlock-ready" {
		t.Errorf("最后一个条目 %q，want tmp/.airlock-ready", order[len(order)-1])
	}
}

func TestCreateRelays(t *testing.T) {
	f := &fakeDocker{reply: newVolumes}
	s := goodSpec()
	if _, err := f.backend().Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, args := f.call(t, "network", "create"); !slices.Equal(args, []string{"network", "create", "--label", "airlock=1", "--internal", "airlock-fake-cabin-in"}) {
		t.Errorf("internal 网络 %v", args)
	}
	_, gate := f.call(t, "run", "-d", "--name", "airlock-fake-cabin-gate")
	joined := strings.Join(gate, " ")
	for _, want := range []string{"--network airlock-fake-cabin-out", "--user nobody", "--cap-drop ALL", "--read-only", "--label airlock=1", relayImage + " TCP-LISTEN:8080,fork,reuseaddr TCP:host.docker.internal:43210"} {
		if !strings.Contains(joined, want) {
			t.Errorf("出网中继参数中没有 %q：%s", want, joined)
		}
	}
	if strings.Contains(joined, "-p ") {
		t.Errorf("出网中继映射了端口：%s", joined)
	}
	if _, args := f.call(t, "network", "connect", "--alias"); !slices.Equal(args, []string{"network", "connect", "--alias", "airlock-gate", "airlock-fake-cabin-in", "airlock-fake-cabin-gate"}) {
		t.Errorf("出网中继接入 internal 网络 %v", args)
	}
	if f.has("run", "-d", "--name", "airlock-fake-cabin-web") {
		t.Error("WebPort 为 0 时创建了入站中继")
	}

	f = &fakeDocker{reply: newVolumes}
	s.WebPort, s.HostPort = 8000, 51234
	if _, err := f.backend().Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	_, web := f.call(t, "run", "-d", "--name", "airlock-fake-cabin-web")
	joined = strings.Join(web, " ")
	for _, want := range []string{"-p 127.0.0.1:51234:8080", "TCP:airlock-fake-cabin-box:8000", "--network airlock-fake-cabin-out"} {
		if !strings.Contains(joined, want) {
			t.Errorf("入站中继参数中没有 %q：%s", want, joined)
		}
	}
}

func TestCreateVolumes(t *testing.T) {
	f := &fakeDocker{reply: newVolumes}
	s := goodSpec()
	if _, err := f.backend().Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	vol := volumeName(s.Name, "node_modules")
	if _, args := f.call(t, "volume", "create"); !slices.Equal(args, []string{"volume", "create", "--label", "airlock-cache=1", "--label", "airlock-cabin=fake-cabin", vol}) {
		t.Errorf("创建卷 %v", args)
	}
	_, chown := f.call(t, "run", "--rm")
	for _, want := range []string{"--label airlock=1", "--user 0:0", "--network none", "--cap-drop ALL --cap-add CHOWN", "type=volume,src=" + vol + ",dst=/v", "chown " + relayImage + " 1000:1000 /v"} {
		if !strings.Contains(strings.Join(chown, " "), want) {
			t.Errorf("修改卷属主的参数中没有 %q：%v", want, chown)
		}
	}

	// 卷已经存在时，不再创建，也不再修改属主。
	f = &fakeDocker{}
	if _, err := f.backend().Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if f.has("volume", "create") || f.has("run", "--rm") {
		t.Error("卷已经存在时又创建了卷")
	}
}

func TestCreateBuildsMissingImage(t *testing.T) {
	f := &fakeDocker{reply: func(args []string) ([]byte, error) {
		if args[0] == "image" {
			return nil, errors.New("fake: no such image")
		}
		return newVolumes(args)
	}}
	s := goodSpec()
	if _, err := f.backend().Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	built := map[string]string{}
	for i, c := range f.calls {
		if c[0] == "build" {
			if !slices.Equal(c, []string{"build", "-t", c[2], "-"}) {
				t.Errorf("构建命令 %v", c)
			}
			built[c[2]] = string(f.stdins[i])
		}
	}
	if built[s.Image] != "FROM fake" || built[relayImage] != relayDockerfile {
		t.Errorf("构建的镜像 %v", built)
	}
}

// 检查镜像出错但不是"不存在"时，直接返回错误，不构建。
func TestCreateImageInspectError(t *testing.T) {
	f := &fakeDocker{reply: func(args []string) ([]byte, error) {
		if args[0] == "image" {
			return nil, errors.New("fake: Cannot connect to the Docker daemon")
		}
		return newVolumes(args)
	}}
	_, err := f.backend().Create(context.Background(), goodSpec())
	if err == nil || !strings.Contains(err.Error(), "Cannot connect") {
		t.Errorf("err = %v，want 检查镜像的错误", err)
	}
	if f.has("build") {
		t.Error("检查镜像出错时构建了镜像")
	}
}

// 任何一步失败时，Create 删除已创建的资源。
func TestCreateCleansUpOnFailure(t *testing.T) {
	steps := [][]string{
		{"build"}, {"network", "create"}, {"run", "-d"}, {"network", "connect"},
		{"volume", "create"}, {"run", "--rm"}, {"create"}, {"start"}, {"exec"},
	}
	for _, step := range steps {
		f := &fakeDocker{}
		f.reply = func(args []string) ([]byte, error) {
			// 前 3 条命令是 Create 开头清理残留的 Remove，之后才让 step 失败。
			if len(f.calls) > 3 && len(args) >= len(step) && slices.Equal(args[:len(step)], step) {
				return nil, errors.New("fake: failed")
			}
			if args[0] == "image" {
				return nil, errors.New("fake: no such image")
			}
			return newVolumes(args)
		}
		if _, err := f.backend().Create(context.Background(), goodSpec()); err == nil {
			t.Errorf("%v 失败时 Create 没有报错", step)
			continue
		}
		last := f.calls[len(f.calls)-1]
		if !slices.Equal(last, []string{"network", "rm", "airlock-fake-cabin-out"}) {
			t.Errorf("%s 失败后最后一条命令 %v，want 删除网络", step, last)
		}
	}
}

func TestCreateRejectsBadSpec(t *testing.T) {
	cases := map[string]func(*Spec){
		"镜像名以连字符开头":       func(s *Spec) { s.Image = "--privileged" },
		"镜像名为空":           func(s *Spec) { s.Image = "" },
		"暂存区是相对路径":        func(s *Spec) { s.StagingDir = "staging" },
		"暂存区路径有逗号":        func(s *Spec) { s.StagingDir += ",dst=/etc" },
		"闸门端口为 0":         func(s *Spec) { s.GatePort = 0 },
		"闸门端口过大":          func(s *Spec) { s.GatePort = 70000 },
		"网页端口没有宿主机端口":     func(s *Spec) { s.WebPort = 8000 },
		"网页端口为负数":         func(s *Spec) { s.WebPort = -1 },
		"缓存目录越出暂存区":       func(s *Spec) { s.Volumes = []string{"../etc"} },
		"缓存目录是绝对路径":       func(s *Spec) { s.Volumes = []string{"/etc"} },
		"缓存目录有逗号":         func(s *Spec) { s.Volumes = []string{"a,dst=/etc"} },
		"缓存目录有反斜杠":        func(s *Spec) { s.Volumes = []string{`a\b`} },
		"缓存目录为空":          func(s *Spec) { s.Volumes = []string{""} },
		"环境变量名有等号":        func(s *Spec) { s.Env = map[string]string{"A=B": "x"} },
		"环境变量名以数字开头":      func(s *Spec) { s.Env = map[string]string{"1A": "x"} },
		"环境变量值有换行":        func(s *Spec) { s.Env = map[string]string{"A": "x\nB=y"} },
		"文件是相对路径":         func(s *Spec) { s.Files = map[string]string{"fake.txt": ""} },
		"文件路径有 ..":        func(s *Spec) { s.Files = map[string]string{"/home/agent/../../etc/passwd": ""} },
		"文件路径是根目录":        func(s *Spec) { s.Files = map[string]string{"/": ""} },
		"文件路径用 ~ 越出 HOME": func(s *Spec) { s.Files = map[string]string{"~/../../etc/x": ""} },
	}
	for name, mutate := range cases {
		s := goodSpec()
		mutate(&s)
		f := &fakeDocker{}
		if _, err := f.backend().Create(context.Background(), s); err == nil {
			t.Errorf("%s：Create 没有报错", name)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s：调用了 docker %v", name, f.calls)
		}
	}
}

func TestEmptyCmdRunsIdle(t *testing.T) {
	s := goodSpec()
	s.Cmd = nil
	args := boxArgs(s, namesOf(s.Name), nil, "fake-env-file")
	if tail := args[len(args)-2:]; !slices.Equal(tail, []string{"sleep", "infinity"}) {
		t.Fatalf("没有 Cmd 时主进程 %v", tail)
	}
}
