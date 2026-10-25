package staging

import "time"

// fileKey 是遍历目录时读到的文件身份信息。文件内容变了，ctime（状态改变时间）一定变：
// agent 可以把大小和修改时间改回原样，但改不了 ctime。所以身份信息没有变化时，可以沿用上次算出的哈希。
type fileKey struct {
	size, mtime, ctime int64 // ctime 是 Unix 纳秒
	id                 int64 // Linux 是 inode，Windows 是创建时间
}

// racyWindow 内改过的文件不缓存哈希。测试中替换它。
var racyWindow = 2 * time.Second

type cachedSum struct {
	key fileKey
	sum string
}

// cached 返回 rel 上次算出的哈希。身份信息变了时 ok 为 false。
func (a *Area) cached(rel string, k fileKey) (sum string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.sums[rel]
	if !ok || c.key != k {
		return "", false
	}
	return c.sum, true
}

// remember 记下 rel 的哈希。k 必须是计算哈希之前读到的，否则计算期间的写入会被漏掉。
func (a *Area) remember(rel string, k fileKey, sum string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sums == nil {
		a.sums = map[string]cachedSum{}
	}
	a.sums[rel] = cachedSum{k, sum}
}
