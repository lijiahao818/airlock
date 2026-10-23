package desktop

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("desktop: 未实现")

func OpenURL(url string) error { return errNotImplemented }

func OpenTerminal(argv []string) error { return errNotImplemented }

// PickFolder 弹出文件夹选择框。用户取消时返回空字符串。
func PickFolder(ctx context.Context) (string, error) { return "", errNotImplemented }
