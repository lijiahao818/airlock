package sandbox

import (
	"context"
	"errors"
	"strings"
)

var (
	errRelayDown = errors.New("出网中继已经停止，agent 不能再连接网络，也不能调用模型。请停止工作舱并审查改动，然后重新启动。")
	errBoxDown   = errors.New("沙箱已经停止，agent 不再运行。可能是 agent 退出了，或者用完了内存。请停止工作舱并审查改动。")
)

// Health 依次检查出网中继和沙箱。docker 不能回答时也按停止处理：这时 agent 同样不能工作。
func (d docker) Health(ctx context.Context, id string) error {
	name, err := nameOfID(id)
	if err != nil {
		return err
	}
	n := namesOf(name)
	for _, c := range []struct {
		name string
		down error
	}{{n.gate, errRelayDown}, {n.box, errBoxDown}} {
		out, err := d.run(ctx, nil, "inspect", "-f", "{{.State.Running}}", c.name)
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			return c.down
		}
	}
	return nil
}
