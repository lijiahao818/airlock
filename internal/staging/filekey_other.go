//go:build !linux && !windows

package staging

import "os"

// keysOf 在其他系统上不读取 ctime，所以不缓存哈希，每次都重新计算。
// shortcut: 只支持 Linux 和 Windows，要支持 macOS 时用 Stat_t.Ctimespec 实现。
func keysOf(root *os.Root, dir string) map[string]fileKey { return nil }
