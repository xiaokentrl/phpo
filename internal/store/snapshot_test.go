// 快照 JSON 形状契约：可空集合一律序列化为 [] / {}，不得出现 null
//
// state:changed 的落地是「一条链」：applySnapshot → applyTaskBoard → task.syncBoard。前端把快照
// 字段当数组/对象直接展开，null 即抛错，抛出即整链中断——队列详情（TaskBrief.Label）随之永不落地，
// 抽屉右栏只能退化成任务 ID（start-2 / stop-1），且必须刷新页面走账本才看到人话标签。
// §5.6.2「事件发了界面却不动」正是本条要挡住的情形，故在快照出口把契约钉死。
package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"phpo/internal/model"
)

// nullCollections 递归收集 JSON 里取值为 null、而 Go 侧类型是切片/映射的字段路径
func nullCollections(t *testing.T, v reflect.Value, raw map[string]json.RawMessage, prefix string) []string {
	t.Helper()
	var out []string
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" || name == "" {
			name = f.Name
		}
		got, ok := raw[name]
		if !ok {
			continue
		}
		path := prefix + name
		switch f.Type.Kind() {
		case reflect.Slice, reflect.Map:
			if string(got) == "null" {
				out = append(out, path)
			}
		case reflect.Struct:
			var sub map[string]json.RawMessage
			if json.Unmarshal(got, &sub) == nil {
				out = append(out, nullCollections(t, v.Field(i), sub, path+".")...)
			}
		}
	}
	return out
}

// TestBuildSnapshot_NoNullCollections 零站点/零扩展的库里跑第一个任务时，快照仍必须是可以被前端
// 逐字段展开的形状，且运行中任务的人话标签要随队列一起出去。
func TestBuildSnapshot_NoNullCollections(t *testing.T) {
	s := openStore(t)
	s.SetTaskBoard(func() model.TaskBoard {
		return model.TaskBoard{
			Running: &model.TaskBrief{ID: "start-2", Label: "启动 phpo-php-8.0", Type: "start", Step: 1, Total: 2},
		}
	})

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if nulls := nullCollections(t, reflect.ValueOf(*snap), fields, ""); len(nulls) > 0 {
		t.Errorf("快照集合字段序列化为 null，前端展开即抛错、state:changed 整链不落地：%s", strings.Join(nulls, "、"))
	}
	if !strings.Contains(string(raw), `"label":"启动 phpo-php-8.0"`) {
		t.Errorf("运行中任务的人话标签未进快照，队列只能显示任务 ID：%s", raw)
	}
}

// TestSnapshot_GapsRideEverySnapshot 缺失态是内存派生态（不落库）但必须随每一帧快照出去：
// 用户用第三方工具删了容器/镜像后，服务卡片要能标出「已被外部删除」，而不是等下一次校准。
func TestSnapshot_GapsRideEverySnapshot(t *testing.T) {
	s := openStore(t)
	s.SetGaps([]model.ServiceGap{{Kind: "php", Version: "8.4", Reason: model.GapImage, Ref: "php:8.4-fpm"}})
	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Gaps) != 1 || snap.Gaps[0].Reason != model.GapImage {
		t.Fatalf("缺失态未进快照: %+v", snap.Gaps)
	}
	s.SetGaps(nil)
	fresh, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Gaps == nil || len(fresh.Gaps) != 0 {
		t.Fatalf("清零后必须是空集合而非 null（前端直接展开）: %+v", fresh.Gaps)
	}
}
