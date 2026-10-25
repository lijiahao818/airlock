package staging

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Go 的 os.FileInfo 在 Windows 上不含 ChangeTime，所以直接调用 GetFileInformationByHandleEx。
// 一次调用返回目录中的一批条目，不需要逐个打开文件。
var procGetFileInformationByHandleEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx")

const (
	fileFullDirectoryInfo        = 14 // FILE_INFO_BY_HANDLE_CLASS 的 FileFullDirectoryInfo
	fileFullDirectoryRestartInfo = 15
	attrDirectory                = 0x10
	attrReparsePoint             = 0x400
	// FILETIME 从 1601 年开始计数，单位是 100 纳秒。这是 1601 年到 1970 年的间隔。
	filetimeEpoch = 116444736000000000
)

// keysOf 返回 root 中 dir 目录下普通文件的身份信息，按文件名索引。读取失败时返回 nil，调用方改为计算哈希。
// 目录经过 root 打开。符号链接和 junction 是重解析点，不列出。
func keysOf(root *os.Root, dir string) map[string]fileKey {
	f, err := root.Open(filepath.FromSlash(dir))
	if err != nil {
		return nil
	}
	defer f.Close()
	// FILE_FULL_DIR_INFO 要求 8 字节对齐，所以用 uint64 分配。
	words := make([]uint64, 8<<10)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	le := binary.LittleEndian
	keys := map[string]fileKey{}
	class := uintptr(fileFullDirectoryRestartInfo)
	for {
		r, _, e := procGetFileInformationByHandleEx.Call(f.Fd(), class, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			if e == syscall.ERROR_NO_MORE_FILES {
				return keys
			}
			return nil
		}
		class = fileFullDirectoryInfo
		// 条目的布局见 FILE_FULL_DIR_INFO：时间和大小从第 8 字节开始，文件名从第 68 字节开始。
		for off := 0; ; {
			ent := buf[off:]
			next := le.Uint32(ent[0:])
			attrs := le.Uint32(ent[56:])
			if attrs&(attrDirectory|attrReparsePoint) == 0 {
				name := make([]uint16, le.Uint32(ent[60:])/2)
				for i := range name {
					name[i] = le.Uint16(ent[68+2*i:])
				}
				keys[string(utf16.Decode(name))] = fileKey{
					id:    int64(le.Uint64(ent[8:])),
					mtime: (int64(le.Uint64(ent[24:])) - filetimeEpoch) * 100,
					ctime: (int64(le.Uint64(ent[32:])) - filetimeEpoch) * 100,
					size:  int64(le.Uint64(ent[40:])),
				}
			}
			if next == 0 {
				break
			}
			off += int(next)
		}
	}
}
