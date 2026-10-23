package webterm

import (
	"context"
	"net/http"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

type Attacher interface {
	Attach(ctx context.Context, id, token string) (sandbox.Session, error)
}

// New 返回网页终端的 HTTP 处理器。origin 是网页终端自己的源，例如 http://127.0.0.1:<端口>。
func New(a Attacher, origin string) http.Handler { return notImplemented }

var notImplemented = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "webterm: 未实现", http.StatusNotImplemented)
})
