// 模型序列化契约测试：manifest.json 的 snake_case 键为 §5.14.2 冻结格式；Snapshot 键对齐前端
package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCacheManifestFrozenKeys(t *testing.T) {
	src := `{
	  "schema_version": 1,
	  "kind": "php",
	  "version": "8.4",
	  "created_at": "2026-09-18T12:00:00Z",
	  "updated_at": "2026-09-18T14:30:00Z",
	  "image": {"name":"php:8.4-fpm","digest":"sha256:abc123","size":450000000,"sha256":"def456","cached_at":"2026-09-18T12:00:00Z"},
	  "apk": [{"name":"libzip-1.10.1.apk","sha256":"aaa111","size":123456,"cached_at":"2026-09-18T12:30:00Z"}],
	  "pecl": [{"name":"redis-6.0.2.tgz","sha256":"bbb222","size":234567,"cached_at":"2026-09-18T13:00:00Z"}]
	}`
	var m CacheManifest
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 1 || m.Kind != "php" || m.Version != "8.4" {
		t.Fatalf("头部字段解析错: %+v", m)
	}
	if m.Image == nil || m.Image.Name != "php:8.4-fpm" || m.Image.Size != 450000000 {
		t.Fatalf("image 解析错: %+v", m.Image)
	}
	if len(m.Apk) != 1 || m.Apk[0].Name != "libzip-1.10.1.apk" {
		t.Fatalf("apk 解析错: %+v", m.Apk)
	}
	if len(m.Pecl) != 1 || m.Pecl[0].Sha256 != "bbb222" {
		t.Fatalf("pecl 解析错: %+v", m.Pecl)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "cached_at", "updated_at", "sha256"} {
		if !strings.Contains(string(out), `"`+key+`"`) {
			t.Errorf("回写丢失冻结键 %q: %s", key, out)
		}
	}
}

func TestSnapshotRoundTripAndDefaults(t *testing.T) {
	s := NewSnapshot()
	s.Installed["php"] = []string{"8.4", "8.3"}
	s.Sites = []Site{{Domain: "demo.test", Port: 80, PHP: "8.4", Root: "~/www/demo.test", Rewrite: "laravel"}}
	s.Env["MYSQL_84_PASSWORD"] = "" // 空密码合法
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.HasVersion("php", "8.4") || got.HasVersion("php", "7.4") {
		t.Error("HasVersion 判定错误")
	}
	if got.Sites[0].Rewrite != "laravel" {
		t.Error("站点字段丢失")
	}
	if v, ok := got.Env["MYSQL_84_PASSWORD"]; !ok || v != "" {
		t.Error("空密码键必须保留（applyStateChange 语义）")
	}
}

func TestTaskStatusAndLogLevelsFrozen(t *testing.T) {
	if len(AllLogLevels) != 5 {
		t.Fatalf("日志行类型必须为 5（§0.3）: %v", AllLogLevels)
	}
	want := []LogLevel{LogCmd, LogMeta, LogOk, LogDim, LogErr}
	for i, w := range want {
		if AllLogLevels[i] != w {
			t.Errorf("行类型[%d] = %q, 期望 %q", i, AllLogLevels[i], w)
		}
	}
	statuses := []TaskStatus{TaskRunning, TaskSuccess, TaskFailed, TaskCancelled}
	if got := TaskFailed; got != "failed" || len(statuses) != 4 {
		t.Error("任务状态必须为 4 态且字面量冻结")
	}
}

func TestPreflightResultJSON(t *testing.T) {
	r := PreflightResult{Ok: true, Warnings: []string{"w1"}, Adjusted: map[string]any{"port": 81}}
	b, _ := json.Marshal(r)
	for _, k := range []string{`"ok":true`, `"warnings":["w1"]`, `"adjusted":{"port":81}`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("载荷键错误 %s in %s", k, b)
		}
	}
}

func TestTimeFormatsInManifest(t *testing.T) {
	ts := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	m := CacheManifest{SchemaVersion: 1, CreatedAt: ts}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), "2026-09-18T12:00:00Z") {
		t.Errorf("时间必须 RFC3339 UTC: %s", b)
	}
}
