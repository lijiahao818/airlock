package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type Spec struct {
	Name       string // 由工作舱 ID 生成，每次运行相同。只含小写字母、数字和连字符
	Image      string
	Dockerfile []byte            // 镜像不存在时用它构建
	StagingDir string            // 宿主机上的暂存区目录，挂载到 /workspace
	Env        map[string]string // 只含占位符和地址，不含真实密钥
	Files      map[string]string // 沙箱内的路径 → 文件内容，agent 启动前写入
	Cmd        []string          // 容器的主进程。为空时运行空闲进程
	GatePort   int               // 闸门在宿主机上的端口，出网中继转发到这里
	WebPort    int               // 沙箱内网页服务的端口。0 表示没有网页界面
	HostPort   int               // 入站中继在宿主机 127.0.0.1 上的端口
	Volumes    []string          // 暂存区内的相对目录，每个挂载为这个工作舱专用的缓存卷
}

// Session 是到沙箱内 agent 会话的一个终端连接。
type Session interface {
	io.ReadWriteCloser // 读：终端输出。写：键盘输入。Close：断开连接，agent 继续运行
	Resize(cols, rows int) error
}

type Backend interface {
	// Check 检查 Docker Desktop 是否安装、是否运行。
	Check(ctx context.Context) error
	// Create 创建网络、中继容器和沙箱，并启动。
	Create(ctx context.Context, s Spec) (id string, err error)
	// Exec 连接运行 cmd 的 agent 会话。会话不存在时启动 cmd。
	Exec(ctx context.Context, id string, cmd []string) (Session, error)
	// AttachCommand 返回在宿主机终端窗口中连接同一个 agent 会话的命令行。
	AttachCommand(id string, cmd []string) []string
	// Logs 返回沙箱主进程的输出。
	Logs(ctx context.Context, id string) (io.ReadCloser, error)
	// Remove 删除沙箱、中继容器和网络。
	Remove(ctx context.Context, id string) error
	// Prune 删除 Airlock 创建的全部残留资源，不删除缓存卷。
	Prune(ctx context.Context) error
	// RemoveCache 删除 Spec.Name 为 name 的工作舱的全部缓存卷。
	RemoveCache(ctx context.Context, name string) error
}

// Docker 返回调用 docker 命令行的 Backend，不引入 Docker SDK。
func Docker() Backend { return docker{run: runDocker, start: startDocker} }

// docker 通过两个函数调用 docker 命令行。单元测试替换它们，不调用真实的 docker。
type docker struct {
	// run 执行一条 docker 命令，返回标准输出。stdin 可以为 nil。
	run func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error)
	// start 返回一条还没启动的长时间运行的 docker 命令，用于 Exec 和 Logs。
	start func(args ...string) *exec.Cmd
}

var _ Backend = docker{}

func runDocker(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		// 错误中只有子命令名和 docker 的报错，不含 stdin 中的文件内容。
		return out.Bytes(), fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

func startDocker(args ...string) *exec.Cmd { return exec.Command("docker", args...) }

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	// 镜像名会作为 docker 的参数，必须以字母或数字开头，防止被当作选项。
	imageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*$`)
	envRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

var errBadName = errors.New("sandbox: 名称只能含小写字母、数字和连字符")

func checkName(name string) error {
	if !nameRe.MatchString(name) {
		return errBadName
	}
	return nil
}

// names 是一个工作舱的全部 docker 资源名。
type names struct {
	box, gate, web, in, out string
}

func namesOf(name string) names {
	p := "airlock-" + name
	return names{box: p + "-box", gate: p + "-gate", web: p + "-web", in: p + "-in", out: p + "-out"}
}

// nameOfID 从沙箱 ID 取回工作舱名称。ID 不是本包创建的沙箱时返回错误，防止操作其他容器。
func nameOfID(id string) (string, error) {
	rest, ok := strings.CutPrefix(id, "airlock-")
	name, ok2 := strings.CutSuffix(rest, "-box")
	if !ok || !ok2 || checkName(name) != nil {
		return "", errors.New("sandbox: 不是 Airlock 创建的沙箱：" + id)
	}
	return name, nil
}

// volumeName 由工作舱名称和目录路径的哈希组成，同一个工作舱的同一个目录每次得到同一个卷。
func volumeName(name, dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return "airlock-cache-" + name + "-" + hex.EncodeToString(sum[:6])
}

const home = "/home/agent"

// filePath 把 Files 的路径转为沙箱内的绝对路径。~/ 表示 /home/agent/。
func filePath(p string) (string, error) {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		p = home + "/" + rest
	}
	if !strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "/" || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("sandbox: 文件路径不合法：%q", p)
	}
	return p, nil
}

// validate 在调用 docker 前检查 Spec。Spec 的值会拼进 docker 的参数和挂载选项。
func validate(s Spec) error {
	if err := checkName(s.Name); err != nil {
		return err
	}
	if !imageRe.MatchString(s.Image) {
		return fmt.Errorf("sandbox: 镜像名不合法：%q", s.Image)
	}
	// 挂载选项用逗号分隔，路径中有逗号或引号会改变挂载的含义。
	if !filepath.IsAbs(s.StagingDir) || strings.ContainsAny(s.StagingDir, `,"`) {
		return fmt.Errorf("sandbox: 暂存区路径不合法：%q", s.StagingDir)
	}
	if s.GatePort < 1 || s.GatePort > 65535 {
		return errors.New("sandbox: 闸门端口不合法")
	}
	if s.WebPort < 0 || s.WebPort > 65535 || s.WebPort > 0 && (s.HostPort < 1 || s.HostPort > 65535) {
		return errors.New("sandbox: 网页端口不合法")
	}
	for _, v := range s.Volumes {
		if v == "" || path.Clean(v) != v || strings.ContainsAny(v, `,"\:`) || !filepath.IsLocal(filepath.FromSlash(v)) {
			return fmt.Errorf("sandbox: 缓存目录不合法：%q", v)
		}
	}
	for k, v := range s.Env {
		if !envRe.MatchString(k) || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("sandbox: 环境变量不合法：%q", k)
		}
	}
	for p := range s.Files {
		if _, err := filePath(p); err != nil {
			return err
		}
	}
	return nil
}
