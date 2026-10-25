package console

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// 浏览器刷新或关闭页面时，启动、停止和重置继续运行，但仍然有超时。
func TestOpsSurviveCanceledRequest(t *testing.T) {
	for _, op := range []string{"start", "stop", "reset"} {
		t.Run(op, func(t *testing.T) {
			e := newEnv(t)
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			e.svc.onOp = func(ctx context.Context) {
				entered <- ctx
				<-release
			}
			defer close(release)
			reqCtx, cancel := context.WithCancel(context.Background())
			req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, e.srv.URL+"/api/cabins/c1/"+op, nil)
			req.Header.Set("X-Airlock-Token", fakeToken)
			go func() {
				if resp, err := e.srv.Client().Do(req); err == nil {
					resp.Body.Close()
				}
			}()
			opCtx := <-entered
			cancel()
			// 服务端发现连接断开需要一点时间。这段时间内操作的 ctx 必须一直有效。
			for range 20 {
				if err := opCtx.Err(); err != nil {
					t.Fatalf("请求中断后操作被取消：%v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, ok := opCtx.Deadline(); !ok {
				t.Fatal("操作没有超时")
			}
		})
	}
}

func TestOpTimeout(t *testing.T) {
	saved := opTimeout
	opTimeout = 50 * time.Millisecond
	t.Cleanup(func() { opTimeout = saved })
	e := newEnv(t)
	expired := make(chan error, 1)
	e.svc.onOp = func(ctx context.Context) {
		<-ctx.Done()
		expired <- ctx.Err()
	}
	resp := e.request(t, http.MethodPost, "/api/cabins/c1/start", "", fakeToken, "", "")
	body(t, resp)
	select {
	case err := <-expired:
		if err != context.DeadlineExceeded {
			t.Fatalf("ctx 结束的原因 = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("操作超时后 ctx 没有结束")
	}
}
