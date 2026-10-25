package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tmuxSession 是沙箱内 agent 所在的 tmux 会话。Exec 和 AttachCommand 都连接它。
const tmuxSession = "agent"

const ttyMarker = "AIRLOCK-TTY "

// 终端设备名来自沙箱的输出，属于不可信数据，必须是这个格式才拼进命令。
var ttyRe = regexp.MustCompile(`^/dev/pts/[0-9]{1,6}$`)

// shellQuote 用单引号包住参数，让 sh 原样看到它。
func shellQuote(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// execArgs 生成 Exec 的 docker 参数。script 分配终端，第一行输出终端设备名。
// stty eof undef 去掉结束符：docker 的输入结束时，script 不会向 agent 发送 ^D。
// LANG：没有 UTF-8 的 locale 时，tmux 认为客户端不支持 UTF-8，把中文显示成下划线。
// 镜像都基于 Debian bookworm，glibc 内置 C.UTF-8，不需要另外安装 locale。
func execArgs(id string, cmd []string) []string {
	inner := `stty eof undef; printf '` + ttyMarker + `%s\n' "$(tty)"; exec tmux new-session -A -s ` + tmuxSession
	if len(cmd) > 0 {
		inner += " " + shellQuote(cmd)
	}
	return []string{"exec", "-i", "-e", "TERM=xterm-256color", "-e", "LANG=C.UTF-8", id, "script", "-qfc", inner, "/dev/null"}
}

func (d docker) AttachCommand(id string, cmd []string) []string {
	if _, err := nameOfID(id); err != nil {
		return nil
	}
	return append([]string{"docker", "exec", "-it", "-e", "TERM=xterm-256color", "-e", "LANG=C.UTF-8", id, "tmux", "new-session", "-A", "-s", tmuxSession}, cmd...)
}

// readTTY 读出第一行中的终端设备名。
func readTTY(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if err != nil {
		return "", fmt.Errorf("sandbox: 没有读到终端设备名：%w", err)
	}
	s := strings.TrimRight(string(line), "\r\n")
	tty, ok := strings.CutPrefix(s, ttyMarker)
	if !ok || !ttyRe.MatchString(tty) {
		return "", fmt.Errorf("sandbox: 终端设备名格式不对：%q", s)
	}
	return tty, nil
}

func (d docker) Exec(ctx context.Context, id string, cmd []string) (Session, error) {
	if _, err := nameOfID(id); err != nil {
		return nil, err
	}
	c := d.start(execArgs(id, cmd)...)
	in, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		return nil, err
	}
	s := &session{d: d, id: id, cmd: c, in: in, out: bufio.NewReader(out)}
	// ctx 只限制连接的时间。连接后会话由 Close 结束，不随 ctx 结束，否则 agent 会被误杀。
	type result struct {
		tty string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		tty, err := readTTY(s.out)
		ch <- result{tty, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			s.kill()
			c.Wait()
			return nil, r.err
		}
		s.tty = r.tty
		return s, nil
	case <-ctx.Done():
		s.kill()
		c.Wait()
		return nil, ctx.Err()
	}
}

type session struct {
	d    docker
	id   string
	tty  string
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	once sync.Once
}

func (s *session) Read(p []byte) (int, error)  { return s.out.Read(p) }
func (s *session) Write(p []byte) (int, error) { return s.in.Write(p) }

func (s *session) Resize(cols, rows int) error {
	if cols < 1 || cols > 1000 || rows < 1 || rows > 1000 {
		return errors.New("sandbox: 终端大小不合法")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.d.run(ctx, nil, "exec", s.id, "stty", "-F", s.tty, "cols", strconv.Itoa(cols), "rows", strconv.Itoa(rows))
	return err
}

// Close 先断开这个终端的 tmux 客户端，再关闭输入。tmux 会话和 agent 继续运行。
// 连接时已经去掉了结束符，关闭输入不会向 agent 发送 ^D。
func (s *session) Close() error {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.d.run(ctx, nil, "exec", s.id, "tmux", "detach-client", "-t", s.tty)
		s.in.Close()
		done := make(chan struct{})
		go func() { s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			s.kill()
			<-done
		}
	})
	return nil
}

func (s *session) kill() {
	s.in.Close()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}

// Logs 返回沙箱主进程的输出，包括标准输出和标准错误。关闭返回值时结束 docker logs。
func (d docker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	if _, err := nameOfID(id); err != nil {
		return nil, err
	}
	c := d.start("logs", "-f", id)
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw
	if err := c.Start(); err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			c.Process.Kill()
		case <-waitDone(c, pw):
		}
	}()
	return &logs{r: pr, cmd: c}, nil
}

// waitDone 在命令结束后关闭管道，让读取方收到 EOF。
func waitDone(c *exec.Cmd, pw *io.PipeWriter) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		pw.CloseWithError(c.Wait())
		close(done)
	}()
	return done
}

type logs struct {
	r   *io.PipeReader
	cmd *exec.Cmd
}

func (l *logs) Read(p []byte) (int, error) { return l.r.Read(p) }

func (l *logs) Close() error {
	l.cmd.Process.Kill()
	return l.r.Close()
}
