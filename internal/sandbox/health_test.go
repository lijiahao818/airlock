package sandbox

import (
	"context"
	"errors"
	"testing"
)

func TestHealth(t *testing.T) {
	const id = "airlock-fake-cabin-box"
	cases := map[string]struct {
		state map[string]string // 容器名 → inspect 的输出。不在表中的容器返回错误
		want  error
	}{
		"都在运行":       {map[string]string{"airlock-fake-cabin-gate": "true\n", "airlock-fake-cabin-box": "true\n"}, nil},
		"中继已经退出":     {map[string]string{"airlock-fake-cabin-gate": "false\n", "airlock-fake-cabin-box": "true\n"}, errRelayDown},
		"中继已经删除":     {map[string]string{"airlock-fake-cabin-box": "true\n"}, errRelayDown},
		"沙箱已经退出":     {map[string]string{"airlock-fake-cabin-gate": "true\n", "airlock-fake-cabin-box": "false\n"}, errBoxDown},
		"docker 不回答": {map[string]string{}, errRelayDown},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeDocker{reply: func(args []string) ([]byte, error) {
				if out, ok := c.state[args[len(args)-1]]; ok {
					return []byte(out), nil
				}
				return nil, errors.New("fake: Error: No such object")
			}}
			if err := f.backend().Health(context.Background(), id); !errors.Is(err, c.want) {
				t.Fatalf("Health = %v, want %v", err, c.want)
			}
		})
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
