package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

var errUnsupported = errors.New("desktop: 只支持 Windows")

// start 启动命令，不等待它结束。单元测试替换它，不启动真实进程。
var start = func(c *exec.Cmd) error {
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	return nil
}

// OpenURL 用默认浏览器打开网址。只接受 http 和 https 网址。
func OpenURL(raw string) error {
	u, err := checkURL(raw)
	if err != nil {
		return err
	}
	c, err := urlCommand(u)
	if err != nil {
		return err
	}
	return start(c)
}

// checkURL 返回规范化的网址。结果不含空白和引号，所以作为命令行参数时不会被加上引号。
func checkURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("desktop: 只能打开 http 或 https 网址：%q", raw)
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == '"' || r == 0x7f }) {
		return "", fmt.Errorf("desktop: 网址含有空白或引号：%q", raw)
	}
	return u.String(), nil
}

// OpenTerminal 打开一个新的终端窗口，运行 argv。argv 的每一项原样传给程序，不经过 shell。
func OpenTerminal(argv []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return errors.New("desktop: 没有要运行的命令")
	}
	c, err := terminalCommand(argv)
	if err != nil {
		return err
	}
	return start(c)
}

// PickFolder 弹出文件夹选择框，返回绝对路径。用户取消时返回空字符串。
func PickFolder(ctx context.Context) (string, error) { return pickFolder(ctx) }
