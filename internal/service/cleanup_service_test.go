// T605c 验收：CleanupService 三模式删除 + 回收站恢复/清空 + 审计 JSON Lines。
//   - Scan：孤儿全量扫描并广播 docker:orphan-found。
//   - Clean：保守/标准/激进裁剪删除集；每删一个广播 docker:cleanup；顺带清空到期回收站；Apply 双写审计。
//   - RestoreTrash：移回原位 + 注销登记；EmptyExpired：仅清到期。
//   - 审计：operations.log 每行可 JSON 解析。
package service

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
)

// ---- 假件 ----

type clDocker struct {
	rep       model.OrphanReport
	removedC  []string
	removedV  []string
	removedN  []string
	removedI  []string
	scanCalls int
}

func (d *clDocker) ScanOrphans(context.Context, map[string]bool) (model.OrphanReport, error) {
	d.scanCalls++
	return d.rep, nil
}
func (d *clDocker) RemoveContainer(_ context.Context, name string) error {
	d.removedC = append(d.removedC, name)
	return nil
}
func (d *clDocker) RemoveVolume(_ context.Context, name string) error {
	d.removedV = append(d.removedV, name)
	return nil
}
func (d *clDocker) RemoveNetwork(_ context.Context, name string) error {
	d.removedN = append(d.removedN, name)
	return nil
}
func (d *clDocker) ImageRemove(_ context.Context, ref string) error {
	d.removedI = append(d.removedI, ref)
	return nil
}

type clStore struct {
	snap   *model.Snapshot
	trash  []store.TrashItem
	ops    []model.Operation
	removd []int64
}

func (s *clStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }
func (s *clStore) ListTrash() ([]store.TrashItem, error)   { return s.trash, nil }
func (s *clStore) RemoveTrashItem(id int64) error {
	s.removd = append(s.removd, id)
	out := s.trash[:0]
	for _, it := range s.trash {
		if it.ID != id {
			out = append(out, it)
		}
	}
	s.trash = out
	return nil
}
func (s *clStore) ExpiredTrash(now time.Time) ([]store.TrashItem, error) {
	var out []store.TrashItem
	for _, it := range s.trash {
		if now.After(it.ExpiresAt) {
			out = append(out, it)
		}
	}
	return out, nil
}
func (s *clStore) AppendOperation(op model.Operation) error {
	s.ops = append(s.ops, op)
	return nil
}
func (s *clStore) ListOperations(int) ([]model.Operation, error) { return s.ops, nil }

type clTrash struct {
	restored [][2]string
	purged   []string
}

func (t *clTrash) Restore(trashPath, origPath string) error {
	t.restored = append(t.restored, [2]string{trashPath, origPath})
	return nil
}
func (t *clTrash) Purge(trashPath string) error {
	t.purged = append(t.purged, trashPath)
	return nil
}

type clCache struct {
	calls  int
	mode   model.CleanupMode
	inUse  map[string]bool
	result *model.CleanupResult
}

func (c *clCache) CleanupCache(_ context.Context, mode model.CleanupMode, inUse map[string]bool, _ int) (*model.CleanupResult, error) {
	c.calls++
	c.mode = mode
	c.inUse = inUse
	if c.result != nil {
		return c.result, nil
	}
	return &model.CleanupResult{Mode: mode, FreedBytes: 4096, Removed: 2}, nil
}

// ---- 装配 ----

func newCleanupSvc(t *testing.T) (*CleanupService, *clDocker, *clStore, *clTrash, *clCache, *fakeEmitter, string) {
	t.Helper()
	dock := &clDocker{rep: model.OrphanReport{
		Containers: []model.DockerResource{
			{Type: model.ResContainer, Name: "phpo-php-7.4", InUse: false},
			{Type: model.ResContainer, Name: "phpo-mysql-5.7", InUse: true},
		},
		Volumes:  []model.DockerResource{{Type: model.ResVolume, Name: "phpo-mysql-5.7-data", Size: 100}},
		Networks: []model.DockerResource{{Type: model.ResNetwork, Name: "phpo-guest"}},
		Images:   []model.DockerResource{{Type: model.ResImage, Name: "phpo/legacy", Size: 200}},
	}}
	st := &clStore{snap: func() *model.Snapshot {
		s := model.NewSnapshot()
		s.Installed["php"] = []string{"8.4"}
		return s
	}()}
	tr := &clTrash{}
	cc := &clCache{}
	em := &fakeEmitter{}
	auditPath := filepath.Join(t.TempDir(), "logs", "operations.log")
	svc := NewCleanupService(dock, st, tr, cc, engine.NewAudit(auditPath), em, task.NewManager(em))
	svc.now = func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }
	return svc, dock, st, tr, cc, em, auditPath
}

func drain(t *testing.T, path string) []model.Operation {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("审计文件应存在: %v", err)
	}
	defer f.Close()
	var out []model.Operation
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var op model.Operation
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			t.Fatalf("审计行不可解析: %q err=%v", line, err)
		}
		out = append(out, op)
	}
	return out
}

// ---- Scan ----

func TestCleanup_Scan_Emits(t *testing.T) {
	svc, _, _, _, _, em, _ := newCleanupSvc(t)
	rep, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total() != 5 {
		t.Fatalf("孤儿总数应为 5，实得 %d", rep.Total())
	}
	if !em.has("docker:orphan-found") {
		t.Fatalf("应广播 docker:orphan-found，实得 %v", em.events)
	}
}

// ---- Clean 三模式 ----

func TestCleanup_Clean_Conservative(t *testing.T) {
	svc, dock, _, _, _, _, _ := newCleanupSvc(t)
	rep, err := svc.Clean(context.Background(), model.CleanupConservative)
	if err != nil {
		t.Fatal(err)
	}
	// 保守：仅已停止孤儿容器（InUse=false），保留运行容器/网络/镜像/卷
	if strings.Join(dock.removedC, ",") != "phpo-php-7.4" {
		t.Fatalf("保守只删停止容器，实得 %v", dock.removedC)
	}
	if len(dock.removedN)+len(dock.removedI)+len(dock.removedV) != 0 {
		t.Fatalf("保守不应删网络/镜像/卷，实得 N=%v I=%v V=%v", dock.removedN, dock.removedI, dock.removedV)
	}
	if rep.Removed != 1 || rep.Failed != 0 {
		t.Fatalf("Removed 应为 1，实得 %+v", rep)
	}
}

func TestCleanup_Clean_Standard(t *testing.T) {
	svc, dock, _, _, _, _, _ := newCleanupSvc(t)
	if _, err := svc.Clean(context.Background(), model.CleanupStandard); err != nil {
		t.Fatal(err)
	}
	// 标准：全部孤儿容器（含运行）+ 网络 + 镜像，保留卷
	if len(dock.removedC) != 2 {
		t.Fatalf("标准应删 2 容器，实得 %v", dock.removedC)
	}
	if strings.Join(dock.removedN, ",") != "phpo-guest" || strings.Join(dock.removedI, ",") != "phpo/legacy" {
		t.Fatalf("标准应删网络+镜像，实得 N=%v I=%v", dock.removedN, dock.removedI)
	}
	if len(dock.removedV) != 0 {
		t.Fatalf("标准应保留卷，实得 %v", dock.removedV)
	}
}

func TestCleanup_Clean_Aggressive(t *testing.T) {
	svc, dock, st, _, _, em, auditPath := newCleanupSvc(t)
	rep, err := svc.Clean(context.Background(), model.CleanupAggressive)
	if err != nil {
		t.Fatal(err)
	}
	if len(dock.removedV) != 1 || dock.removedV[0] != "phpo-mysql-5.7-data" {
		t.Fatalf("激进应删孤儿卷，实得 %v", dock.removedV)
	}
	if rep.Removed != 5 {
		t.Fatalf("激进删除 5 项，实得 %d", rep.Removed)
	}
	if rep.FreedBytes != 300 { // 卷 100 + 镜像 200
		t.Fatalf("FreedBytes 应为 300，实得 %d", rep.FreedBytes)
	}
	if !em.has("docker:cleanup") || !em.has("state:changed") {
		t.Fatalf("应广播 docker:cleanup + state:changed，实得 %v", em.events)
	}
	// 审计落表 + 文件可解析
	ops := drain(t, auditPath)
	if len(ops) != 1 || ops[0].Op != "cleanup" {
		t.Fatalf("应有 1 条 cleanup 审计，实得 %+v", ops)
	}
	if len(st.ops) != 1 {
		t.Fatalf("operations 表应有 1 条，实得 %d", len(st.ops))
	}
}

// ---- 回收站 ----

func TestCleanup_TrashRestore(t *testing.T) {
	svc, _, st, tr, _, _, _ := newCleanupSvc(t)
	st.trash = []store.TrashItem{
		{ID: 7, Kind: "volume", OrigPath: "~/phpo/mysql/5.7/data", TrashPath: "/trash/7", ExpiresAt: svc.now().Add(48 * time.Hour)},
	}
	// 未过期，ListTrash 应标 Expired=false
	list, err := svc.ListTrash()
	if err != nil || len(list) != 1 || list[0].Expired {
		t.Fatalf("未过期不应标 Expired，实得 %+v err=%v", list, err)
	}
	if err := svc.RestoreTrash(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(tr.restored) != 1 || tr.restored[0] != [2]string{"/trash/7", "~/phpo/mysql/5.7/data"} {
		t.Fatalf("应移回原位，实得 %v", tr.restored)
	}
	if len(st.removd) != 1 || st.removd[0] != 7 {
		t.Fatalf("应注销登记，实得 %v", st.removd)
	}
	// 不存在的条目应报错
	if err := svc.RestoreTrash(context.Background(), 999); err == nil {
		t.Fatal("恢复不存在条目应报错")
	}
}

func TestCleanup_EmptyExpired_OnlyExpired(t *testing.T) {
	svc, _, st, tr, _, _, auditPath := newCleanupSvc(t)
	now := svc.now()
	st.trash = []store.TrashItem{
		{ID: 1, OrigPath: "old", TrashPath: "/trash/1", ExpiresAt: now.Add(-time.Hour)},  // 到期
		{ID: 2, OrigPath: "fresh", TrashPath: "/trash/2", ExpiresAt: now.Add(time.Hour)}, // 未到期
	}
	n, err := svc.EmptyExpired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("仅清到期 1 条，实得 %d", n)
	}
	if strings.Join(tr.purged, ",") != "/trash/1" {
		t.Fatalf("应永久删除到期条目，实得 %v", tr.purged)
	}
	ops := drain(t, auditPath)
	if len(ops) != 1 || ops[0].Op != "trash-empty" {
		t.Fatalf("应有 trash-empty 审计，实得 %+v", ops)
	}
}

// ---- 缓存清理（复用 cache 三模式，inUse 由快照推导）----

func TestCleanup_CleanCache(t *testing.T) {
	svc, _, _, _, cc, _, auditPath := newCleanupSvc(t)
	res, err := svc.CleanCache(context.Background(), model.CleanupAggressive)
	if err != nil {
		t.Fatal(err)
	}
	if cc.calls != 1 || cc.mode != model.CleanupAggressive {
		t.Fatalf("应透传模式，实得 calls=%d mode=%s", cc.calls, cc.mode)
	}
	if !cc.inUse["php/8.4"] {
		t.Fatalf("inUse 应含已安装 php/8.4，实得 %v", cc.inUse)
	}
	if res == nil || res.Removed != 2 {
		t.Fatalf("应返回缓存清理结果，实得 %+v", res)
	}
	ops := drain(t, auditPath)
	if len(ops) != 1 || ops[0].Op != "cache-cleanup" {
		t.Fatalf("应有 cache-cleanup 审计，实得 %+v", ops)
	}
}
