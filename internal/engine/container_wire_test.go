// §0.2 规则 20 门禁：删除容器只删容器本体——不得带 v=1（删卷），否则卸载/重装会连数据一起抹
// 同时覆盖 §5.13.1 一致性的启动侧：崩溃循环要走 restart 强制立刻重试，失败要把容器日志真因带进报错
package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

// fakeDaemon 记录 SDK 实际下发到守护进程的请求，并按脚本回放容器状态
type fakeDaemon struct {
	deleteQuery  string
	started      int    // POST /containers/{name}/start 次数
	restarted    int    // POST /containers/{name}/restart 次数
	stopped      int    // POST /containers/{name}/stop 次数
	stopErr      string // 非空则 POST /stop 回 500 并带上这句（模拟引擎收尾抱怨）
	deleteErr    string // 非空则 DELETE /containers/{name} 回 500 并带上这句
	inspectState map[string]bool
	statusSeq    map[string][]string // 逐次 inspect 吐出的状态；耗尽后沿用最后一个；"absent" 回 404
	logBody      string              // GET /containers/{id}/logs 的响应体
	t            *testing.T
}

// nextStatus 弹出该容器下一次 inspect 的状态；未脚本化时回落到 inspectState 的布尔判定
func (f *fakeDaemon) nextStatus(name string) string {
	seq := f.statusSeq[name]
	if len(seq) == 0 {
		if f.inspectState[name] {
			return "running"
		}
		return "exited"
	}
	status := seq[0]
	if len(seq) > 1 {
		f.statusSeq[name] = seq[1:]
	}
	return status
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	tail := func(prefix string) string { // 取 /containers/{name} 之后的片段
		i := strings.LastIndex(path, prefix)
		if i < 0 {
			return ""
		}
		return strings.Trim(path[i+len(prefix):], "/")
	}
	switch {
	case strings.HasSuffix(path, "/_ping"):
		w.Header().Set("API-Version", "1.51")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/json"):
		name := strings.TrimSuffix(tail("/containers/"), "/json")
		status := f.nextStatus(name)
		// 哨兵值 "absent"：回放「库里已经没有这个容器」——SDK 侧即 errdefs IsNotFound
		if status == "absent" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "No such container: " + name})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":    "abc123",
			"State": map[string]any{"Status": status, "Running": status == "running", "ExitCode": 1},
		})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/logs"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(f.logBody))
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/start"):
		f.started++
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/stop"):
		f.stopped++
		if f.stopErr != "" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": f.stopErr})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/restart"):
		f.restarted++
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && strings.Contains(path, "/containers/"):
		f.deleteQuery = r.URL.RawQuery
		if f.deleteErr != "" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": f.deleteErr})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Fatalf("未预期的守护进程请求: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
	}
}

func newFakeClient(t *testing.T, daemon http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(daemon)
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost(srv.URL), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return &Client{cli: cli}
}

func TestRemoveContainer_KeepsVolumes(t *testing.T) {
	d := &fakeDaemon{t: t, inspectState: map[string]bool{"phpo-mysql-8.4": true}}
	c := newFakeClient(t, d)
	if err := c.RemoveContainer(context.Background(), "phpo-mysql-8.4"); err != nil {
		t.Fatalf("删除容器失败: %v", err)
	}
	q := d.deleteQuery
	if !strings.Contains(q, "force=1") {
		t.Fatalf("删除应带 force（停删合一）: %q", q)
	}
	// RemoveVolumes 会让 SDK 下发 v=1，Docker 随即删除该容器的匿名卷——卸载默认保留数据，不得下发
	if strings.Contains(q, "v=1") {
		t.Fatalf("删除容器不得请求删卷，实得 query=%q", q)
	}
}

func TestRemoveContainer_MissingIsNoop(t *testing.T) {
	d := &fakeDaemon{t: t, inspectState: map[string]bool{}}
	c := newFakeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			d.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "No such container"})
			return
		}
		d.ServeHTTP(w, r)
	}))
	if err := c.RemoveContainer(context.Background(), "phpo-mysql-9.9"); err != nil {
		t.Fatalf("不存在应视为已删除: %v", err)
	}
	if d.deleteQuery != "" {
		t.Fatalf("不存在不应下发 DELETE: %q", d.deleteQuery)
	}
}

// shrinkStartWait 把启动就绪等待收到毫秒级：崩溃循环的取证路径要跑到超时，用真参数会拖满 startWait
func shrinkStartWait(t *testing.T) {
	t.Helper()
	w, i, h := startWait, startInterval, startHold
	startWait, startInterval, startHold = 400*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { startWait, startInterval, startHold = w, i, h })
}

// frame 按 Docker 非 tty 日志流格式打帧：[流别][3 字节零][大端 4 字节长度][负载]
func frame(stream byte, s string) string {
	hdr := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(s)))
	return string(hdr) + s
}

func TestStartContainer_RunningIsNoop(t *testing.T) {
	shrinkStartWait(t)
	d := &fakeDaemon{t: t, statusSeq: map[string][]string{"phpo-redis-8": {"running"}}}
	c := newFakeClient(t, d)
	if err := c.StartContainer(context.Background(), "phpo-redis-8"); err != nil {
		t.Fatalf("已在运行应直接返回: %v", err)
	}
	if d.started != 0 || d.restarted != 0 {
		t.Fatalf("已在运行不该下发 start/restart: start=%d restart=%d", d.started, d.restarted)
	}
}

// 真机取证：崩溃循环中的容器状态是 restarting，且 Docker 的自动重试退避已拉到 60s；
// 下发 start 只排进下次重试，等待窗口内永远读不到 running——必须 restart 强制立刻再起一次
func TestStartContainer_CrashLoopForcesImmediateRestart(t *testing.T) {
	shrinkStartWait(t)
	d := &fakeDaemon{t: t, statusSeq: map[string][]string{"phpo-pgsql-17": {"restarting", "running", "running"}}}
	c := newFakeClient(t, d)
	if err := c.StartContainer(context.Background(), "phpo-pgsql-17"); err != nil {
		t.Fatalf("重启循环里的容器应被强制再起并成功: %v", err)
	}
	if d.restarted != 1 {
		t.Fatalf("崩溃循环应下发一次 restart，实得 %d", d.restarted)
	}
	if d.started != 0 {
		t.Fatalf("崩溃循环不应下发 start（只会排进分钟级退避），实得 %d", d.started)
	}
}

// 起来即崩：等到超时才判失败，且报错要带状态、退出码与容器日志尾部（§3.2 原则 3——错误要是人话）
func TestStartContainer_CrashReportsLogTail(t *testing.T) {
	shrinkStartWait(t)
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-pgsql-17": {"exited"}},
		logBody: frame(1, "LOG:  database system is shut down") +
			frame(2, `FATAL:  could not open log file "/var/log/postgresql/postgresql-2026-09-22.log": Permission denied`),
	}
	c := newFakeClient(t, d)
	err := c.StartContainer(context.Background(), "phpo-pgsql-17")
	if err == nil {
		t.Fatal("始终 exited 必须报错")
	}
	for _, want := range []string{"phpo-pgsql-17", "exited", "退出码 1", "Permission denied", "database system is shut down"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("报错应含 %q，实得 %q", want, err)
		}
	}
}

// 停止侧：restarting 的容器仍会被 Docker 拉起，只认 running 会漏停，「已停止」下一轮即被推翻
func TestStopContainer_CrashLoopStillStops(t *testing.T) {
	d := &fakeDaemon{t: t, statusSeq: map[string][]string{"phpo-pgsql-17": {"restarting"}}}
	c := newFakeClient(t, d)
	if err := c.StopContainer(context.Background(), "phpo-pgsql-17"); err != nil {
		t.Fatalf("停止崩溃循环容器失败: %v", err)
	}
	if d.stopped != 1 {
		t.Fatalf("restarting 也要下发 stop，实得 %d", d.stopped)
	}
}

// 真机取证（Podman 5.7 rootless）：容器确实已经停下，报错来自停止之后的网络收尾
// `removing container <id> network: 1 error occurred: * rootless netns: kill network process: permission denied`。
// 判据：报错后现读一次现场，实测已停＝目标达成，把引擎原文带走当提醒，不判死这一步。
func TestStopContainer_EngineComplainsAfterAlreadyStopped(t *testing.T) {
	netns := "Error response from daemon: removing container 5264afb582e5 network: " +
		"1 error occurred:\n\t* rootless netns: kill network process: permission denied"
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-php-8.0": {"running", "exited"}},
		stopErr:   netns,
	}
	c := newFakeClient(t, d)
	err := c.StopContainer(context.Background(), "phpo-php-8.0")
	if err == nil {
		t.Fatal("引擎报错必须留痕，不能当没事发生")
	}
	tol := AsTolerated(err)
	if tol == nil {
		t.Fatalf("实测已停应判为「目标达成 + 提醒」，实得普通错误: %v", err)
	}
	if tol.Name != "phpo-php-8.0" || !strings.Contains(tol.Msg, "rootless netns") {
		t.Fatalf("提醒要点名容器并带上引擎原文，实得 %+v", tol)
	}
	if d.stopped != 1 {
		t.Fatalf("应下发一次 stop，实得 %d", d.stopped)
	}
}

// 反向判据：报错后现读仍在跑＝真没停下，原判失败照旧，不能借「收尾抱怨」把停不下的服务说成停了
func TestStopContainer_StillRunningAfterErrorIsFailure(t *testing.T) {
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-php-8.0": {"running", "running"}},
		stopErr:   "permission denied",
	}
	c := newFakeClient(t, d)
	err := c.StopContainer(context.Background(), "phpo-php-8.0")
	if err == nil {
		t.Fatal("容器还在跑必须判失败")
	}
	if AsTolerated(err) != nil {
		t.Fatalf("实测仍在跑不得算达成，实得 %v", err)
	}
}

// 现场问不出来（第二次 inspect 也报错）时保守判失败：不能把「看不见」当成「已停」
func TestStopContainer_UnreadableStateIsFailure(t *testing.T) {
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-php-8.0": {"running", "running"}},
		stopErr:   "connection refused",
	}
	c := newFakeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			d.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json") && d.stopped > 0 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "cannot connect to podman"})
			return
		}
		d.ServeHTTP(w, r)
	}))
	err := c.StopContainer(context.Background(), "phpo-php-8.0")
	if err == nil || AsTolerated(err) != nil {
		t.Fatalf("现场问不出来时应原样判失败，实得 %v", err)
	}
}

// 删除侧同一把尺子：DELETE 报错但现读容器已经不在了＝目标达成，引擎原文作提醒
func TestRemoveContainer_EngineComplainsAfterAlreadyGone(t *testing.T) {
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-php-8.0": {"running", "absent"}},
		deleteErr: "rootless netns: kill network process: permission denied",
	}
	c := newFakeClient(t, d)
	err := c.RemoveContainer(context.Background(), "phpo-php-8.0")
	tol := AsTolerated(err)
	if tol == nil {
		t.Fatalf("容器真没了应判达成 + 提醒，实得 %v", err)
	}
	if tol.Op != "remove" || !strings.Contains(tol.Msg, "rootless netns") {
		t.Fatalf("提醒要标出是哪一步并带上引擎原文，实得 %+v", tol)
	}
	if !strings.Contains(d.deleteQuery, "force=1") {
		t.Fatalf("删除应带 force: %q", d.deleteQuery)
	}
}

// 反向判据：DELETE 报错且容器还在库里＝真失败，不得折成达成
func TestRemoveContainer_StillPresentAfterErrorIsFailure(t *testing.T) {
	d := &fakeDaemon{t: t,
		statusSeq: map[string][]string{"phpo-php-8.0": {"running", "running"}},
		deleteErr: "device or resource busy",
	}
	c := newFakeClient(t, d)
	err := c.RemoveContainer(context.Background(), "phpo-php-8.0")
	if err == nil || AsTolerated(err) != nil {
		t.Fatalf("容器还在时必须判失败，实得 %v", err)
	}
}

// ToleratedAsNil 只折「实测已达成」那一类；真失败要原样上抛，别把提醒助手当成万能吞错
func TestToleratedAsNil_KeepsRealFailures(t *testing.T) {
	if err := ToleratedAsNil(&ToleratedError{Name: "phpo-php-8.0", Op: "stop", Msg: "收尾抱怨"}); err != nil {
		t.Fatalf("已达成那一类应折成 nil，实得 %v", err)
	}
	boom := errors.New("真的失败")
	if err := ToleratedAsNil(fmt.Errorf("停止容器: %w", boom)); err == nil {
		t.Fatal("包装过的真失败不得被吞掉")
	}
	if AsTolerated(fmt.Errorf("停止容器: %w", &ToleratedError{Name: "x"})) == nil {
		t.Fatal("包装过一层的容错错误也要认得出来")
	}
}
