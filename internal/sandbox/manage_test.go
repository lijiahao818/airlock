package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"testing"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{nil, nil},
		{fmt.Errorf("docker version: %w", exec.ErrNotFound), errNoDocker},
		{errors.New("fake: cannot connect to the Docker daemon"), errDockerDown},
	}
	for _, c := range cases {
		f := &fakeDocker{reply: func([]string) ([]byte, error) { return nil, c.err }}
		if err := f.backend().Check(context.Background()); err != c.want {
			t.Errorf("docker 返回 %v 时 Check = %v，want %v", c.err, err, c.want)
		}
	}
	if errNoDocker.Error() == errDockerDown.Error() {
		t.Error("两种情况的错误信息相同")
	}
}

// PATH 中没有 docker 时，真实的 Check 报告"没有找到 docker 命令"，不会调用 docker。
func TestCheckWithoutDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Docker().Check(context.Background()); err != errNoDocker {
		t.Fatalf("Check = %v，want errNoDocker", err)
	}
}

func TestRemove(t *testing.T) {
	f := &fakeDocker{reply: func(args []string) ([]byte, error) {
		return nil, errors.New("Error: No such container: fake")
	}}
	if err := f.backend().Remove(context.Background(), "airlock-fake-cabin-box"); err != nil {
		t.Fatalf("资源不存在时 Remove = %v", err)
	}
	want := [][]string{
		{"rm", "-f", "airlock-fake-cabin-box", "airlock-fake-cabin-gate", "airlock-fake-cabin-web"},
		{"network", "rm", "airlock-fake-cabin-in"},
		{"network", "rm", "airlock-fake-cabin-out"},
	}
	if !slices.EqualFunc(f.calls, want, slices.Equal) {
		t.Errorf("Remove 的命令 %v", f.calls)
	}
	for _, step := range []string{"rm", "network"} {
		f = &fakeDocker{reply: func(args []string) ([]byte, error) {
			if args[0] == step {
				return nil, errors.New("fake: daemon error")
			}
			return nil, nil
		}}
		if err := f.backend().Remove(context.Background(), "airlock-fake-cabin-box"); err == nil {
			t.Errorf("docker %s 出错时 Remove 没有报错", step)
		}
	}
	f = &fakeDocker{}
	for _, id := range []string{"fake-other", "airlock-fake-cabin-gate"} {
		if err := f.backend().Remove(context.Background(), id); err == nil {
			t.Errorf("Remove(%q) 没有报错", id)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("不合法的 ID 调用了 docker：%v", f.calls)
	}
}

func TestPrune(t *testing.T) {
	f := &fakeDocker{reply: func(args []string) ([]byte, error) {
		switch {
		case args[0] == "ps":
			return []byte("c1\nc2\n"), nil
		case args[0] == "network" && args[1] == "ls":
			return []byte("n1\n"), nil
		}
		return nil, nil
	}}
	if err := f.backend().Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ps", "-aq", "--filter", "label=airlock=1"},
		{"rm", "-f", "c1", "c2"},
		{"network", "ls", "-q", "--filter", "label=airlock=1"},
		{"network", "rm", "n1"},
	}
	if !slices.EqualFunc(f.calls, want, slices.Equal) {
		t.Errorf("Prune 的命令 %v", f.calls)
	}
	for _, c := range f.calls {
		if c[0] == "volume" {
			t.Errorf("Prune 操作了卷：%v", c)
		}
	}
	// 没有残留时不调用 rm。
	f = &fakeDocker{}
	if err := f.backend().Prune(context.Background()); err != nil || len(f.calls) != 2 {
		t.Errorf("没有残留时 Prune = %v，命令 %v", err, f.calls)
	}
	for _, step := range [][]string{{"ps"}, {"rm"}, {"network", "ls"}, {"network", "rm"}} {
		f = &fakeDocker{}
		f.reply = func(args []string) ([]byte, error) {
			if slices.Equal(args[:len(step)], step) {
				return nil, errors.New("fake: daemon error")
			}
			if args[0] == "ps" || args[1] == "ls" {
				return []byte("x\n"), nil
			}
			return nil, nil
		}
		if err := f.backend().Prune(context.Background()); err == nil {
			t.Errorf("docker %v 出错时 Prune 没有报错", step)
		}
	}
}

func TestRemoveCache(t *testing.T) {
	f := &fakeDocker{reply: func(args []string) ([]byte, error) {
		if args[1] == "ls" {
			return []byte("v1\nv2\n"), nil
		}
		return nil, nil
	}}
	if err := f.backend().RemoveCache(context.Background(), "fake-cabin"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"volume", "ls", "-q", "--filter", "label=airlock-cache=1", "--filter", "label=airlock-cabin=fake-cabin"},
		{"volume", "rm", "v1", "v2"},
	}
	if !slices.EqualFunc(f.calls, want, slices.Equal) {
		t.Errorf("RemoveCache 的命令 %v", f.calls)
	}
	f = &fakeDocker{reply: func([]string) ([]byte, error) { return nil, errors.New("fake: daemon error") }}
	if err := f.backend().RemoveCache(context.Background(), "fake-cabin"); err == nil {
		t.Error("docker 出错时 RemoveCache 没有报错")
	}
}
