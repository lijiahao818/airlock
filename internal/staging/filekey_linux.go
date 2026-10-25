package staging

import (
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// keysOf 返回 root 中 dir 目录下普通文件的身份信息，按文件名索引。读取失败时返回 nil，调用方改为计算哈希。
// 每个文件都经过 root 读取，不跟随符号链接。
func keysOf(root *os.Root, dir string) map[string]fileKey {
	f, err := root.Open(filepath.FromSlash(dir))
	if err != nil {
		return nil
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil
	}
	keys := map[string]fileKey{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := root.Lstat(filepath.FromSlash(path.Join(dir, e.Name())))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		keys[e.Name()] = fileKey{size: st.Size, mtime: st.Mtim.Nano(), ctime: st.Ctim.Nano(), id: int64(st.Ino)}
	}
	return keys
}
