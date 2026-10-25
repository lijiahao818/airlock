package sandbox

import (
	"context"
	"fmt"
	"strings"
)

// StoppedError 表示 docker 确认出网中继或沙箱已经停止。Error 返回给用户看的中文说明和下一步操作。
// 检查本身失败（例如 docker 暂时没有响应）时，Health 返回其他错误，调用方应该稍后重试，不能当作已经停止。
type StoppedError struct{ Msg string }

func (e *StoppedError) Error() string { return e.Msg }

var (
	errRelayDown = &StoppedError{"出网中继已经停止，agent 不能再连接网络，也不能调用模型。请停止工作舱并审查改动，然后重新启动。"}
	errBoxDown   = &StoppedError{"沙箱已经停止，agent 不再运行。可能是 agent 退出了，或者用完了内存。请停止工作舱并审查改动。"}
)

// Health 依次检查出网中继和沙箱。
func (d docker) Health(ctx context.Context, id string) error {
	name, err := nameOfID(id)
	if err != nil {
		return err
	}
	n := namesOf(name)
	for _, c := range []struct {
		name string
		down *StoppedError
	}{{n.gate, errRelayDown}, {n.box, errBoxDown}} {
		out, err := d.run(ctx, nil, "inspect", "-f", "{{.State.Running}}", c.name)
		switch {
		// 容器被删除时 docker 回答 "Error: no such object"，旧版本首字母大写。
		case err != nil && strings.Contains(strings.ToLower(err.Error()), "no such object"):
			return c.down
		case err != nil:
			return fmt.Errorf("sandbox: 检查 %s 失败：%w", c.name, err)
		case strings.TrimSpace(string(out)) != "true":
			return c.down
		}
	}
	return nil
}
