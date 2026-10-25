package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	const id = "airlock-fake-cabin-box"
	missing := errors.New("docker inspect: exit status 1: Error: no such object: x")
	busy := errors.New("docker inspect: context deadline exceeded: ")
	cases := map[string]struct {
		state map[string]string // 容器名 → inspect 的输出。不在表中的容器返回 err
		err   error
		want  error // nil 表示正常；errRelayDown、errBoxDown 表示确认停止
	}{
		"都在运行":          {map[string]string{"airlock-fake-cabin-gate": "true\n", "airlock-fake-cabin-box": "true\n"}, nil, nil},
		"中继已经退出":        {map[string]string{"airlock-fake-cabin-gate": "false\n", "airlock-fake-cabin-box": "true\n"}, nil, errRelayDown},
		"中继已经删除":        {map[string]string{"airlock-fake-cabin-box": "true\n"}, missing, errRelayDown},
		"沙箱已经退出":        {map[string]string{"airlock-fake-cabin-gate": "true\n", "airlock-fake-cabin-box": "false\n"}, nil, errBoxDown},
		"沙箱已经删除（旧版本报错）": {map[string]string{"airlock-fake-cabin-gate": "true\n"}, errors.New("Error: No such object: x"), errBoxDown},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeDocker{reply: func(args []string) ([]byte, error) {
				if out, ok := c.state[args[len(args)-1]]; ok {
					return []byte(out), nil
				}
				return nil, c.err
			}}
			if err := f.backend().Health(context.Background(), id); err != c.want {
				t.Fatalf("Health = %v, want %v", err, c.want)
			}
		})
	}
	// 说明给用户看，要说清楚发生了什么和下一步操作。
	for _, e := range []error{errRelayDown, errBoxDown} {
		if msg := e.Error(); !strings.Contains(msg, "已经停止") || !strings.Contains(msg, "请停止工作舱并审查改动") {
			t.Errorf("说明 = %q", msg)
		}
	}
	// docker 暂时没有响应时，不能当作已经停止，否则会误报。
	f := &fakeDocker{reply: func([]string) ([]byte, error) { return nil, busy }}
	err := f.backend().Health(context.Background(), id)
	var stopped *StoppedError
	if err == nil || errors.As(err, &stopped) {
		t.Fatalf("docker 没有响应时 Health = %v，不应该是 StoppedError", err)
	}
}

// 不是 Airlock 创建的沙箱时不调用 docker，防止检查其他容器。
func TestHealthRejectsForeignID(t *testing.T) {
	f := &fakeDocker{}
	if err := f.backend().Health(context.Background(), "fake-other"); err == nil {
		t.Fatal("应该拒绝不是 Airlock 创建的沙箱")
	}
	if len(f.calls) != 0 {
		t.Fatalf("调用了 docker：%v", f.calls)
	}
}
