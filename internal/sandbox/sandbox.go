package sandbox

import (
	"context"
	"errors"
	"io"
)

var errNotImplemented = errors.New("sandbox: 未实现")

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
func Docker() Backend { return docker{} }

type docker struct{}

var _ Backend = docker{}

func (docker) Check(ctx context.Context) error                    { return errNotImplemented }
func (docker) Create(ctx context.Context, s Spec) (string, error) { return "", errNotImplemented }
func (docker) Exec(ctx context.Context, id string, cmd []string) (Session, error) {
	return nil, errNotImplemented
}
func (docker) AttachCommand(id string, cmd []string) []string { return nil }
func (docker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return nil, errNotImplemented
}
func (docker) Remove(ctx context.Context, id string) error        { return errNotImplemented }
func (docker) Prune(ctx context.Context) error                    { return errNotImplemented }
func (docker) RemoveCache(ctx context.Context, name string) error { return errNotImplemented }
