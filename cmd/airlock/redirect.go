package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// redirected 检查写入 base 下 name 目录的文件是否被 Windows 转存到打包应用的私有目录。
// 从 MSIX 打包应用（例如 Claude 桌面应用）的终端启动时，写入 %LOCALAPPDATA% 的文件被转存到
// %LOCALAPPDATA%\Packages\<包名>\LocalCache\Local\ 下。Docker Desktop 看不到这些文件，
// 沙箱中因此没有项目文件（Issue #64）。这个进程没有包身份，所以只能看写入的实际结果。
func redirected(base, name string) (bool, error) {
	dir := filepath.Join(base, name)
	os.MkdirAll(dir, 0o700) // 失败时 CreateTemp 也失败，并返回错误
	f, err := os.CreateTemp(dir, ".redirect-check-")
	if err != nil {
		return false, err
	}
	f.Close()
	defer os.Remove(f.Name())
	return inPackage(base, filepath.Join(name, filepath.Base(f.Name())))
}

// inPackage 检查 base 下的 rel 是否出现在某个打包应用的私有目录中。
func inPackage(base, rel string) (bool, error) {
	pkgs, err := os.ReadDir(filepath.Join(base, "Packages"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, p := range pkgs {
		// 其他应用的私有目录可能不允许读取。这类错误不说明文件被转存，所以只看是否找到。
		if _, err := os.Lstat(filepath.Join(base, "Packages", p.Name(), "LocalCache", "Local", rel)); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// blocked 在数据目录 dir 不能使用时输出原因和下一步操作，返回 true。r 和 err 是 redirected 的结果。
func blocked(stderr io.Writer, dir string, r bool, err error) bool {
	switch {
	case err != nil:
		fmt.Fprintf(stderr, "Airlock 启动失败：数据目录 %s 不能写入：%v\n请检查这个目录的权限，然后再试一次。\n", dir, err)
	case r:
		fmt.Fprintln(stderr, "Airlock 不能在这个窗口中运行：这个窗口属于另一个应用（例如 Claude 桌面应用），Windows 会把 Airlock 的数据转存到那个应用的私有目录，沙箱因此看不到你的项目文件。\n请关闭这个窗口，在资源管理器中双击 airlock.exe 启动 Airlock。")
	default:
		return false
	}
	return true
}
