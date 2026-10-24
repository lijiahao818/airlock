package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

var (
	errNoDocker   = errors.New("没有找到 docker 命令。请安装 Docker Desktop，安装完成后重新打开 Airlock。")
	errDockerDown = errors.New("Docker Desktop 没有运行。请启动 Docker Desktop，等它显示 Engine running 后再试。")
)

func (d docker) Check(ctx context.Context) error {
	_, err := d.run(ctx, nil, "version", "--format", "{{.Server.Version}}")
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errNoDocker
	case err != nil:
		return errDockerDown
	}
	return nil
}

// Remove 删除沙箱、中继容器和网络。资源已经不存在时不报错。
func (d docker) Remove(ctx context.Context, id string) error {
	name, err := nameOfID(id)
	if err != nil {
		return err
	}
	n := namesOf(name)
	if _, err := d.run(ctx, nil, "rm", "-f", n.box, n.gate, n.web); err != nil && !notFound(err) {
		return err
	}
	for _, net := range []string{n.in, n.out} {
		if _, err := d.run(ctx, nil, "network", "rm", net); err != nil && !notFound(err) {
			return err
		}
	}
	return nil
}

// notFound 判断 docker 的错误是不是"资源不存在"。
func notFound(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such") || strings.Contains(s, "not found")
}

// Prune 按标签 airlock=1 删除全部容器和网络。缓存卷带的是另一个标签，不受影响。
func (d docker) Prune(ctx context.Context) error {
	out, err := d.run(ctx, nil, "ps", "-aq", "--filter", "label=airlock=1")
	if err != nil {
		return err
	}
	if ids := strings.Fields(string(out)); len(ids) > 0 {
		if _, err := d.run(ctx, nil, append([]string{"rm", "-f"}, ids...)...); err != nil {
			return err
		}
	}
	out, err = d.run(ctx, nil, "network", "ls", "-q", "--filter", "label=airlock=1")
	if err != nil {
		return err
	}
	if ids := strings.Fields(string(out)); len(ids) > 0 {
		if _, err := d.run(ctx, nil, append([]string{"network", "rm"}, ids...)...); err != nil {
			return err
		}
	}
	return nil
}

// RemoveCache 删除工作舱 name 的全部缓存卷。
func (d docker) RemoveCache(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	out, err := d.run(ctx, nil, "volume", "ls", "-q", "--filter", "label=airlock-cache=1", "--filter", "label=airlock-cabin="+name)
	if err != nil {
		return err
	}
	if vols := strings.Fields(string(out)); len(vols) > 0 {
		_, err = d.run(ctx, nil, append([]string{"volume", "rm"}, vols...)...)
	}
	return err
}
