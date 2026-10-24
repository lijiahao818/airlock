package sandbox

import (
	"context"
	"errors"
	"io"
)

var errNotImplemented = errors.New("sandbox: 未实现")

func (docker) Exec(ctx context.Context, id string, cmd []string) (Session, error) {
	return nil, errNotImplemented
}
func (docker) AttachCommand(id string, cmd []string) []string { return nil }
func (docker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return nil, errNotImplemented
}
