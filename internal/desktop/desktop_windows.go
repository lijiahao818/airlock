package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
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

// pickScript 是固定的脚本，不拼接任何输入。路径以不带 BOM 的 UTF-8 输出。
const pickScript = `Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$owner = New-Object System.Windows.Forms.Form -Property @{TopMost = $true}
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = '选择项目文件夹'
if ($dialog.ShowDialog($owner) -eq 'OK') { [Console]::Out.Write($dialog.SelectedPath) }`

func pickFolder(ctx context.Context) (string, error) {
	ps, err := system32("WindowsPowerShell", "v1.0", "powershell.exe")
	if err != nil {
		return "", err
	}
	// -EncodedCommand 的值是 UTF-16LE 的 base64，脚本不经过命令行的引号处理。
	u := utf16.Encode([]rune(pickScript))
	b := make([]byte, 2*len(u))
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	c := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	out, err := c.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("desktop: 文件夹选择框出错：%w", err)
	}
	return parsePicked(string(out))
}

// parsePicked 检查选择框的输出。空输出表示用户取消。
func parsePicked(out string) (string, error) {
	p := strings.TrimRight(out, "\r\n")
	if p == "" {
		return "", nil
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("desktop: 选择框返回的不是绝对路径：%q", p)
	}
	return p, nil
}
