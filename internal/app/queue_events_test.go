// 队列详情实时回流回归（§5.6.1）：任务一进入运行态，装配层注入的队列 watcher 就必须把
// 「带人话标签的权威快照」推给前端——抽屉右栏的标签只有这一个来源，task:* 事件不带标签。
package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

// labeledBoards 从捕获的事件里取出每次 state:changed 携带的队列详情（按前端实际收到的 JSON 形状）
func labeledBoards(t *testing.T, em *CapturingEmitter) []model.TaskBoard {
	t.Helper()
	var out []model.TaskBoard
	for _, ev := range em.Capture() {
		if ev.Name != EventStateChanged {
			continue
		}
		raw, err := json.Marshal(ev.Payload)
		if err != nil {
			t.Fatalf("state:changed 载荷无法序列化: %v", err)
		}
		var p struct {
			Snapshot *model.Snapshot `json:"snapshot"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("state:changed 载荷不合协议: %v", err)
		}
		if p.Snapshot == nil {
			t.Fatal("state:changed 载荷缺 snapshot 字段")
		}
		out = append(out, p.Snapshot.Tasks)
	}
	return out
}

func TestQueueWatcher_PushesLabeledBoardOnStart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "work")
	www := filepath.Join(t.TempDir(), "www")
	cfg, err := config.LoadConfigStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoots(home, www); err != nil {
		t.Fatal(err)
	}

	c := NewContainer()
	em := &CapturingEmitter{}
	c.Emitter = em // 必须先于 object-graph 钩子：任务管理器与队列 watcher 都从这里取发射器
	c.Build()
	ctx := context.Background()
	if err := runHook(t, c.Lifecycle, "object-graph"); err != nil {
		t.Fatalf("object-graph 应成功: %v", err)
	}
	t.Cleanup(func() { c.Lifecycle.OnShutdown(ctx) })

	// Docker 缺席时本单会以失败收口，与用例无关：要验的是「开始那一刻推了什么」
	_ = c.AppService.Start(ctx, model.KindPHP, "8.0")

	for _, b := range labeledBoards(t, em) {
		if b.Running != nil && strings.HasPrefix(b.Running.Label, "启动 ") {
			return
		}
	}
	t.Fatal("没有任何一次 state:changed 把运行中任务的人话标签推给前端（抽屉队列只能退化成任务 ID）")
}
