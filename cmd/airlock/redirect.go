package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// redirected 检查写入 dir 的文件是否被 Windows 转存到打包应用的私有目录。dir 必须在 base 下。
// 从 MSIX 打包应用（例如 Claude 桌面应用）的终端启动时，写入 %LOCALAPPDATA% 的文件被转存到
// %LOCALAPPDATA%\Packages\<包名>\LocalCache\Local\ 下。Docker Desktop 看不到这些文件，
// 沙箱中因此没有项目文件（Issue #64）。这个进程没有包身份，所以只能看写入的实际结果。
func redirected(base, dir string) (bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	f, err := os.CreateTemp(dir, ".redirect-check-")
	if err != nil {
		return false, err
	}
	f.Close()
	defer os.Remove(f.Name())
	rel, err := filepath.Rel(base, f.Name())
	if err != nil {
		return false, err
	}
	return inPackage(base, rel)
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
		if _, err := os.Lstat(filepath.Join(base, "Packages", p.Name(), "LocalCache", "Local", rel)); err == nil {
			return true, nil
		}
	}
	return false, nil
}
