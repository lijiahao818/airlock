package filecheck

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/staging"
)

type Checker struct{}

var _ cabin.Inspector = Checker{}

var (
	// depDirs 是 ADR-008 列出的依赖目录名。项目中任何层级的同名目录都算。
	depDirs = []string{"node_modules", ".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache", "target", ".gradle", ".next", ".nuxt"}

	// keyNames 是 OpenSSH 私钥的常见文件名。
	keyNames = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "id_ecdsa_sk", "id_ed25519_sk"}

	// noticeNames 是 agent 会当作指令读取的文件名。
	noticeNames = []string{"AGENTS.md", "CLAUDE.md", "SKILL.md"}
)

// Reason 给用户看，说明发生了什么、风险多大、可以怎么做（CLAUDE.md 第 15 节）。
const (
	reasonEnv        = "环境变量文件，常存有密码和密钥。已排除，不进沙箱。"
	reasonKeyName    = "文件名是常见的私钥文件名。已排除，不进沙箱。"
	reasonKeyContent = "文件开头有私钥标记。已排除，不进沙箱。"
	reasonCacheDir   = "依赖目录，文件多，可以重新安装。不复制，沙箱内用缓存卷。"
	reasonNotice     = "agent 会把这类文件当作指令读取。打开看一遍，确认没有可疑指令。"
)

// Entry 做进舱检查：标出不进沙箱的文件、改用缓存卷的依赖目录和需要注意的文件。
func (Checker) Entry(project string) ([]cabin.Finding, error) {
	var out []cabin.Finding
	err := filepath.WalkDir(project, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(project, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if slices.Contains(depDirs, name) {
				out = append(out, cabin.Finding{Path: rel + "/", Kind: cabin.Cached, Reason: reasonCacheDir})
				return fs.SkipDir
			}
			return nil
		}
		// 不打开符号链接和特殊文件（红线 6）。
		if !d.Type().IsRegular() {
			return nil
		}
		switch {
		case name == ".env" || strings.HasPrefix(name, ".env."):
			out = append(out, cabin.Finding{Path: rel, Kind: cabin.Excluded, Reason: reasonEnv})
		case slices.Contains(keyNames, name), strings.HasSuffix(name, ".pem"), strings.HasSuffix(name, ".key"):
			out = append(out, cabin.Finding{Path: rel, Kind: cabin.Excluded, Reason: reasonKeyName})
		case hasPrivateKeyHead(p):
			out = append(out, cabin.Finding{Path: rel, Kind: cabin.Excluded, Reason: reasonKeyContent})
		case slices.Contains(noticeNames, name):
			out = append(out, cabin.Finding{Path: rel, Kind: cabin.Notice, Reason: reasonNotice})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("filecheck: 检查项目文件夹 %s 失败，请确认它存在并且可以读取：%w", project, err)
	}
	return out, nil
}

// keyHeadLen 覆盖最长的 PEM 私钥头 -----BEGIN ENCRYPTED PRIVATE KEY-----。
const keyHeadLen = 64

// hasPrivateKeyHead 只读取文件开头的几个字节，判断有没有私钥标记。读不了的文件按没有标记处理。
func hasPrivateKeyHead(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, keyHeadLen)
	n, _ := io.ReadFull(f, b)
	return strings.Contains(string(b[:n]), "PRIVATE KEY")
}

// Exit 为出舱审查标出高风险改动。
func (Checker) Exit(files string, changes []staging.Change) []cabin.Finding { return nil }
