package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// holdingGate 是一个假的闸门：/hold 的请求一直挂着，直到 release 关闭，模拟等待用户决定的暂扣。
func holdingGate(t *testing.T) (port int, active *atomic.Int32, release func()) {
	t.Helper()
	active = &atomic.Int32{}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			active.Add(1)
			defer active.Add(-1)
			select {
			case <-done:
			case <-r.Context().Done():
			}
		}
		fmt.Fprint(w, "fake-gate ok")
	}))
	var once atomic.Bool
	release = func() {
		if once.CompareAndSwap(false, true) {
			close(done)
		}
	}
	// Cleanup 后进先出：先放开挂着的请求，再关闭服务器。
	t.Cleanup(srv.Close)
	t.Cleanup(release)
	return srv.Listener.Addr().(*net.TCPAddr).Port, active, release
}

// holdInBox 在沙箱中后台发起 n 个经过出网中继的请求。setsid 让它们在 docker exec 结束后继续运行。
func holdInBox(t *testing.T, id string, n int) {
	t.Helper()
	script := "setsid sh -c 'for i in $(seq " + strconv.Itoa(n) + "); do wget -q -O /dev/null -T 300 http://airlock-gate:8080/hold & done; wait' >/dev/null 2>&1 &"
	if out, ok := inBox(t, id, script); !ok {
		t.Fatalf("发起请求失败：%s", out)
	}
}

func waitActive(t *testing.T, active *atomic.Int32, n int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for active.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("2 分钟内只有 %d 个请求到达闸门，want %d", active.Load(), n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func relayRunning(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// 300 个暂扣同时等待时，出网中继保持运行，模型请求仍能通过（#84 AC-1）。
// socat 只按 TCP 转发，不看 HTTP 方法，所以这里用普通 GET 挂住连接，效果与 CONNECT 相同。
// CONNECT 和模型路由的语义由 gate 包的 TestRouteDuringManyHolds 覆盖。
// 超过并发上限时，中继暂停接受新连接，主进程不退出；连接数降下来后，新的连接可以建立（AC-2）。
func TestDockerRelayConcurrency(t *testing.T) {
	needDocker(t)
	port, active, release := holdingGate(t)
	s := baseSpec(t)
	s.GatePort = port
	id := createBox(t, s)
	gate := namesOf(s.Name).gate

	holdInBox(t, id, 300)
	waitActive(t, active, 300)
	if !relayRunning(t, gate) {
		t.Fatal("300 个连接同时等待时，出网中继退出了")
	}
	if out, ok := inBox(t, id, "wget -q -O - -T 20 http://airlock-gate:8080/ | grep -q 'fake-gate ok'"); !ok {
		t.Fatalf("300 个连接同时等待时，模型请求失败：%s", out)
	}

	// 再发起 190 个，总数超过 relayMaxChildren。
	holdInBox(t, id, 190)
	waitActive(t, active, relayMaxChildren)
	time.Sleep(3 * time.Second)
	// 实测 socat 1.8.0.0 在达到 max-children 时还会多接受一个连接，所以最多 relayMaxChildren+1 个，仍低于进程数上限。
	// 另外 9 个连接在排队，没有到达闸门。
	if n := active.Load(); n > relayMaxChildren+1 {
		t.Fatalf("同时转发了 %d 个连接，超过上限 %d", n, relayMaxChildren+1)
	}
	if !relayRunning(t, gate) {
		t.Fatal("连接数超过上限时，出网中继退出了")
	}

	// 放开全部请求后，排队的连接和新的连接都能建立。
	release()
	if out, ok := inBox(t, id, "wget -q -O - -T 60 http://airlock-gate:8080/ | grep -q 'fake-gate ok'"); !ok {
		t.Fatalf("连接数降下来后，新的连接失败：%s", out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Docker().Health(ctx, id); err != nil {
		t.Fatalf("Health = %v", err)
	}
}

// 出网中继或沙箱停止后，Health 返回对应的中文说明（#84 AC-3）。
func TestDockerHealth(t *testing.T) {
	needDocker(t)
	s := baseSpec(t)
	id := createBox(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := Docker().Health(ctx, id); err != nil {
		t.Fatalf("刚创建时 Health = %v", err)
	}
	if out, err := exec.Command("docker", "stop", "-t", "1", namesOf(s.Name).gate).CombinedOutput(); err != nil {
		t.Fatalf("停止中继：%v %s", err, out)
	}
	if err := Docker().Health(ctx, id); !errors.Is(err, errRelayDown) {
		t.Fatalf("中继停止后 Health = %v", err)
	}
	if out, err := exec.Command("docker", "start", namesOf(s.Name).gate).CombinedOutput(); err != nil {
		t.Fatalf("重新启动中继：%v %s", err, out)
	}
	if err := Docker().Health(ctx, id); err != nil {
		t.Fatalf("中继重新启动后 Health = %v", err)
	}
	if out, err := exec.Command("docker", "stop", "-t", "1", id).CombinedOutput(); err != nil {
		t.Fatalf("停止沙箱：%v %s", err, out)
	}
	if err := Docker().Health(ctx, id); !errors.Is(err, errBoxDown) {
		t.Fatalf("沙箱停止后 Health = %v", err)
	}
}
