package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// 中继镜像只做 TCP 转发，不持有密钥。与团队验证 docs/security.md 第 6 节时使用的镜像相同。
// 基础镜像按 digest 锁定。socat 不锁版本，因为 Alpine 的分支仓库只保留最新的包。
// 修改 relayDockerfile 时，同时修改 relayImage 的标签，否则会继续使用已构建的旧镜像。
const (
	relayImage      = "airlock-relay:alpine3.20-d9e853e8"
	relayDockerfile = "FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc\nRUN apk add --no-cache socat\nUSER nobody\nENTRYPOINT [\"socat\"]\n"
)

// readyFile 写入后，沙箱的主进程才运行 Spec.Cmd。
const readyFile = "/tmp/.airlock-ready"

// waitScript 是沙箱的主进程：等 Files 写入完成，再运行 Spec.Cmd。
const waitScript = `while [ ! -e ` + readyFile + ` ]; do sleep 0.2; done; exec "$@"`

// hardening 是沙箱和中继共用的加固参数，见 docs/adr/001-isolation-backend.md。
var hardening = []string{"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--pull", "never"}

// Create 创建网络、中继容器和沙箱，并启动。任何一步失败时，删除已创建的资源。
func (d docker) Create(ctx context.Context, s Spec) (string, error) {
	if err := validate(s); err != nil {
		return "", err
	}
	n := namesOf(s.Name)
	// 先清理上次运行的残留，Create 可以重复调用。
	if err := d.Remove(ctx, n.box); err != nil {
		return "", err
	}
	if err := d.create(ctx, s, n); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d.Remove(cleanup, n.box)
		return "", err
	}
	return n.box, nil
}

func (d docker) create(ctx context.Context, s Spec, n names) error {
	if err := d.ensureImage(ctx, relayImage, []byte(relayDockerfile)); err != nil {
		return err
	}
	if err := d.ensureImage(ctx, s.Image, s.Dockerfile); err != nil {
		return err
	}
	for _, net := range [][]string{{"--internal", n.in}, {n.out}} {
		if _, err := d.run(ctx, nil, slices.Concat([]string{"network", "create", "--label", "airlock=1"}, net)...); err != nil {
			return err
		}
	}
	if err := d.relay(ctx, n.gate, n, nil, "TCP:host.docker.internal:"+strconv.Itoa(s.GatePort), "airlock-gate"); err != nil {
		return err
	}
	if s.WebPort > 0 {
		publish := []string{"-p", "127.0.0.1:" + strconv.Itoa(s.HostPort) + ":8080"}
		if err := d.relay(ctx, n.web, n, publish, "TCP:"+n.box+":"+strconv.Itoa(s.WebPort), ""); err != nil {
			return err
		}
	}
	mounts, err := d.volumes(ctx, s)
	if err != nil {
		return err
	}
	envFile, err := writeEnvFile(s.Env)
	if err != nil {
		return err
	}
	_, err = d.run(ctx, nil, boxArgs(s, n, mounts, envFile)...)
	os.Remove(envFile)
	if err != nil {
		return err
	}
	if _, err := d.run(ctx, nil, "start", n.box); err != nil {
		return err
	}
	archive, err := filesTar(s.Files)
	if err != nil {
		return err
	}
	_, err = d.run(ctx, bytes.NewReader(archive), "exec", "-i", n.box, "tar", "-x", "-C", "/", "-f", "-")
	return err
}

// ensureImage 在镜像不存在时用 dockerfile 构建它。构建不需要上下文目录。其他错误直接返回。
func (d docker) ensureImage(ctx context.Context, image string, dockerfile []byte) error {
	_, err := d.run(ctx, nil, "image", "inspect", "--format", "{{.Id}}", image)
	if err == nil || !notFound(err) {
		return err
	}
	_, err = d.run(ctx, bytes.NewReader(dockerfile), "build", "-t", image, "-")
	return err
}

// 中继的并发连接上限。暂扣中的连接最长占用一个子进程 60 秒，依赖下载和模型的流式响应也会同时占用子进程。
const (
	relayMaxChildren = 480
	relayPids        = 512
)

// relay 启动一个中继：在 out 网络上运行 socat，再接入 internal 网络。alias 不为空时，作为 internal 网络上的别名。
func (d docker) relay(ctx context.Context, name string, n names, publish []string, target, alias string) error {
	args := slices.Concat(
		[]string{"run", "-d", "--name", name, "--label", "airlock=1", "--network", n.out, "--user", "nobody"},
		hardening,
		// socat 为每个连接 fork 一个子进程。子进程数达到进程数上限时，fork 失败，socat 主进程退出（#84）。
		// max-children 让 socat 在子进程数达到上限时暂停接受新连接，主进程不退出。上限低于 --pids-limit，留出余量。
		[]string{"--pids-limit", strconv.Itoa(relayPids), "--memory", "256m"},
		publish,
		[]string{relayImage, "TCP-LISTEN:8080,fork,reuseaddr,max-children=" + strconv.Itoa(relayMaxChildren), target},
	)
	if _, err := d.run(ctx, nil, args...); err != nil {
		return err
	}
	connect := []string{"network", "connect"}
	if alias != "" {
		connect = append(connect, "--alias", alias)
	}
	_, err := d.run(ctx, nil, append(connect, n.in, name)...)
	return err
}

// volumes 确保每个缓存目录有一个卷，返回挂载参数。新卷的属主改为沙箱用户，否则 agent 不能写入。
func (d docker) volumes(ctx context.Context, s Spec) ([]string, error) {
	var mounts []string
	for _, dir := range s.Volumes {
		vol := volumeName(s.Name, dir)
		if _, err := d.run(ctx, nil, "volume", "inspect", vol); err != nil {
			if _, err := d.run(ctx, nil, "volume", "create", "--label", "airlock-cache=1", "--label", "airlock-cabin="+s.Name, vol); err != nil {
				return nil, err
			}
			// 以 root 运行，只给 CHOWN 能力，不接入网络，改完即删除。被中断时，Prune 按标签清理残留的容器。
			chown := []string{"run", "--rm", "--label", "airlock=1", "--user", "0:0", "--network", "none", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--pull", "never",
				"--mount", "type=volume,src=" + vol + ",dst=/v", "--entrypoint", "chown", relayImage, "1000:1000", "/v"}
			if _, err := d.run(ctx, nil, chown...); err != nil {
				return nil, err
			}
		}
		mounts = append(mounts, "--mount", "type=volume,src="+vol+",dst=/workspace/"+dir)
	}
	return mounts, nil
}

// boxArgs 生成创建沙箱的 docker 参数。Env 和 Files 不在参数中。
func boxArgs(s Spec, n names, mounts []string, envFile string) []string {
	cmd := s.Cmd
	if len(cmd) == 0 {
		cmd = []string{"sleep", "infinity"}
	}
	return slices.Concat(
		[]string{"create", "--name", n.box, "--label", "airlock=1", "--network", n.in, "--user", "1000:1000", "--init"},
		hardening,
		[]string{
			"--pids-limit", "512", "--memory", "4g",
			"--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=1g",
			"--tmpfs", home + ":rw,nosuid,nodev,size=1g,uid=1000,gid=1000,mode=0700",
			"--env", "HOME=" + home,
			"--env-file", envFile,
			"--mount", "type=bind,src=" + s.StagingDir + ",dst=/workspace",
		},
		mounts,
		[]string{"-w", "/workspace", "--entrypoint", "sh", s.Image, "-c", waitScript, "sh"},
		cmd,
	)
}

// writeEnvFile 把 Env 写入临时文件。在 Unix 上文件权限是 0600；在 Windows 上，权限取决于 %TEMP% 的 ACL。
// Env 只含占位符和地址，不含真实密钥。
func writeEnvFile(env map[string]string) (string, error) {
	f, err := os.CreateTemp("", "airlock-env-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(env)) {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// filesTar 把 Files 打包为 tar，最后加上 readyFile。
func filesTar(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	add := func(p, content string) error {
		h := &tar.Header{Name: strings.TrimPrefix(p, "/"), Mode: 0o600, Size: int64(len(content)), Uid: 1000, Gid: 1000, ModTime: time.Unix(0, 0)}
		if err := w.WriteHeader(h); err != nil {
			return err
		}
		_, err := w.Write([]byte(content))
		return err
	}
	for _, p := range slices.Sorted(maps.Keys(files)) {
		abs, err := filePath(p)
		if err != nil {
			return nil, err
		}
		if err := add(abs, files[p]); err != nil {
			return nil, err
		}
	}
	if err := add(readyFile, ""); err != nil {
		return nil, err
	}
	// 先关闭，tar 的结尾块才会写入 buf。
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
