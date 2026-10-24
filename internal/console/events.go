package console

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// events 以 text/event-stream 输出运行事件。客户端断开时调用 Subscribe 返回的 cancel。
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	events, cancel := s.deps.Cabins.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	// 立即发送响应头，这样前端的 fetch 马上拿到响应，不用等第一个事件。
	_ = rc.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-events:
			if !ok {
				// 通道被关闭（cancel 会关闭它），结束这条连接。
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}
