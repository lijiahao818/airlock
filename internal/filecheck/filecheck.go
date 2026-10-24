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
	// 名单和匹配一律用小写：宿主机的文件系统不区分大小写，
	// 沙箱可以用大小写变体绕过区分大小写的匹配。

	// depDirs 是 ADR-008 列出的依赖目录名。项目中任何层级的同名目录都算。
	depDirs = []string{"node_modules", ".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache", "target", ".gradle", ".next", ".nuxt"}

	// keyNames 是 OpenSSH 私钥的常见文件名。
	keyNames = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "id_ecdsa_sk", "id_ed25519_sk"}

	// noticeNames 是 agent 会当作指令读取的文件名。
	noticeNames = []string{"agents.md", "claude.md", "skill.md"}

	// ciDirs 是 CI 配置所在的目录前缀。
	ciDirs = []string{".github/workflows/", ".github/actions/", ".circleci/"}

	// hookDirs 是项目中的 Git 钩子目录前缀。
	hookDirs = []string{".husky/"}

	// ciFiles 是常见 CI 系统的配置文件名。
	ciFiles = []string{"jenkinsfile", ".gitlab-ci.yml", ".travis.yml"}

	// installScripts 是依赖安装脚本的文件名。
	installScripts = []string{"install.sh", "install.bash", "setup.sh", "bootstrap.sh", "setup.py", "preinstall.sh", "postinstall.sh"}
)

// Reason 给用户看，说明发生了什么、风险多大、可以怎么做（CLAUDE.md 第 15 节）。
const (
	reasonEnv        = "环境变量文件，常存有密码和密钥。已排除，不进沙箱。"
	reasonSymlink    = "符号链接或特殊文件，可能指向项目外面。已排除，不进沙箱。"
	reasonKeyName    = "文件名是常见的私钥文件名。已排除，不进沙箱。"
	reasonKeyContent = "文件开头有私钥标记。已排除，不进沙箱。"
	reasonCacheEntry = "依赖目录，文件多，可以重新安装。不复制，沙箱内用缓存卷。"
	reasonNotice     = "agent 会把这类文件当作指令读取。打开看一遍，确认没有可疑指令。"
	reasonCacheExit  = "依赖目录下的改动，由沙箱内的缓存卷保管。不写回项目。"
	reasonGitHook    = "Git 钩子，提交或推送代码时会自动运行。确认内容可信再应用。"
	reasonGit        = "Git 仓库数据，Git 命令运行时会自动执行其中的内容。确认内容可信再应用。"
	reasonCI         = "CI 配置，推送代码后自动运行。确认没有可疑步骤再应用。"
	reasonInstall    = "依赖安装脚本，运行时会下载和执行代码。确认内容可信再应用。"
	reasonExecutable = "可执行文件，没法当作文本逐行看。确认来源可靠再应用。"
	reasonBadPath    = "路径想指向项目外面。先弄清这个文件的来源，再决定。"
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
		name := strings.ToLower(d.Name())
		if d.IsDir() {
			// .git 里是压缩对象，私钥嗅探不可能命中。跳过整个目录，进舱检查更快。
			if name == ".git" {
				return fs.SkipDir
			}
			if slices.Contains(depDirs, name) {
				out = append(out, cabin.Finding{Path: rel + "/", Kind: cabin.Cached, Reason: reasonCacheEntry})
				return fs.SkipDir
			}
			return nil
		}
		// 符号链接和特殊文件可能指向项目外面。不跟随、不读取目标，按默认拒绝排除（红线 3、红线 6）。
		if !d.Type().IsRegular() {
			out = append(out, cabin.Finding{Path: rel, Kind: cabin.Excluded, Reason: reasonSymlink})
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

// sniffLen 是私钥嗅探读取的字节数。openssl pkcs12 导出的文件以 Bag Attributes
// 开头，PRIVATE KEY 标记出现在 100 字节以后。
const sniffLen = 1024

// hasPrivateKeyHead 只读取文件开头的几个字节，判断有没有私钥标记。读不了的文件按没有标记处理。
func hasPrivateKeyHead(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, b)
	return strings.Contains(string(b[:n]), "PRIVATE KEY")
}

// Exit 为出舱审查标出高风险改动和依赖目录的改动。改动清单来自沙箱，判定时先排除异常路径（红线 6）。
func (Checker) Exit(files string, changes []staging.Change) []cabin.Finding {
	var out []cabin.Finding
	for _, c := range changes {
		if f, ok := judge(files, c); ok {
			out = append(out, f)
		}
	}
	return out
}

// judge 对一条改动给出最多一条结果。
func judge(files string, c staging.Change) (cabin.Finding, bool) {
	if badPath(c.Path) {
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonBadPath}, true
	}
	lower := strings.ToLower(c.Path)
	parts := strings.Split(lower, "/")
	if slices.ContainsFunc(parts, func(e string) bool { return slices.Contains(depDirs, e) }) {
		return cabin.Finding{Path: c.Path, Kind: cabin.Cached, Reason: reasonCacheExit}, true
	}
	name := parts[len(parts)-1]
	switch {
	// .git/config 的 core.hooksPath 和 core.fsmonitor 跟 hook 一样能执行代码，整个 .git 都标高风险。
	case slices.Contains(parts, ".git"):
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonGit}, true
	case slices.ContainsFunc(hookDirs, func(d string) bool { return strings.HasPrefix(lower, d) }):
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonGitHook}, true
	case slices.ContainsFunc(ciDirs, func(d string) bool { return strings.HasPrefix(lower, d) }),
		slices.Contains(ciFiles, name):
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonCI}, true
	case slices.Contains(installScripts, name):
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonInstall}, true
	// 删除的文件读不到文件头，只按名字规则处理。
	case c.Kind != staging.Deleted && !c.Special && isExecutable(files, c.Path):
		return cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: reasonExecutable}, true
	}
	return cabin.Finding{}, false
}

// badPath 判断路径是否想越过暂存目录：绝对路径、盘符、反斜杠、.. 或空路径。这种改动不读取文件。
func badPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return slices.Contains(strings.Split(p, "/"), "..")
}

// isExecutable 按文件头判断是不是可执行文件。只读开头几个字节，不跟随符号链接（红线 6）。
// 改动路径来自沙箱，中间目录可能是符号链接。os.Root 保证读取不逃出暂存目录。
func isExecutable(files, rel string) bool {
	root, err := os.OpenRoot(files)
	if err != nil {
		return false
	}
	defer root.Close()
	p := filepath.FromSlash(rel)
	info, err := root.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := root.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	// 打开前后文件可能被换成符号链接。核对是同一个文件再读（红线 6）。
	fi, err := f.Stat()
	if err != nil || !os.SameFile(info, fi) {
		return false
	}
	b := make([]byte, 4)
	n, _ := io.ReadFull(f, b)
	head := string(b[:n])
	return strings.HasPrefix(head, "\x7fELF") ||
		strings.HasPrefix(head, "MZ") ||
		strings.HasPrefix(head, "#!")
}
