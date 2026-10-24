package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// createNewConsole 让子进程打开自己的控制台窗口，见 https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags。
const createNewConsole = 0x00000010

// system32 返回系统目录中的程序的绝对路径，不从 PATH 或当前目录查找。
func system32(name ...string) (string, error) {
	root := os.Getenv("SystemRoot")
	if !filepath.IsAbs(root) {
		return "", errors.New("desktop: 找不到 Windows 系统目录")
	}
	return filepath.Join(append([]string{root, "System32"}, name...)...), nil
}

func urlCommand(u string) (*exec.Cmd, error) {
	rundll32, err := system32("rundll32.exe")
	if err != nil {
		return nil, err
	}
	return exec.Command(rundll32, "url.dll,FileProtocolHandler", u), nil
}

func terminalCommand(argv []string) (*exec.Cmd, error) {
	c := exec.Command(argv[0], argv[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	return c, nil
}
