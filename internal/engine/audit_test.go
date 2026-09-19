// T605a · 审计 JSON Lines：写多条 → 逐行反解析回 model.Operation；目录不存在自动创建；空路径报错不静默丢
package engine

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"phpo/internal/model"
)

func TestAudit_LogAppendAndParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "operations.log")
	a := NewAudit(path)

	ops := []model.Operation{
		{TS: time.Unix(1700000000, 0).UTC(), Actor: "ui", Op: "cleanup", Args: map[string]any{"mode": "standard"}, Status: "success", DurationMs: 12},
		{TS: time.Unix(1700000001, 0).UTC(), Actor: "ui", Op: "trash-restore", Args: map[string]any{"id": 7}, Status: "failed", Error: "boom"},
	}
	for _, op := range ops {
		if err := a.Log(op); err != nil {
			t.Fatalf("写入审计失败: %v", err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("日志文件应存在: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var got []model.Operation
	for sc.Scan() {
		var op model.Operation
		if err := json.Unmarshal(sc.Bytes(), &op); err != nil {
			t.Fatalf("审计行应可解析为 JSON: %v (行=%q)", err, sc.Text())
		}
		got = append(got, op)
	}
	if len(got) != len(ops) {
		t.Fatalf("行数应=%d，实得 %d", len(ops), len(got))
	}
	if got[0].Op != "cleanup" || got[0].Status != "success" {
		t.Fatalf("首条字段不符: %+v", got[0])
	}
	if got[1].Error != "boom" || got[1].Actor != "ui" {
		t.Fatalf("次条字段不符: %+v", got[1])
	}
}

func TestAudit_EmptyPathErrors(t *testing.T) {
	if err := (&Audit{}).Log(model.Operation{Op: "x"}); err == nil {
		t.Fatal("空路径应返回错误而非静默丢弃")
	}
}

func TestAuditLine_RoundTrip(t *testing.T) {
	op := model.Operation{TS: time.Unix(1700000000, 0).UTC(), Actor: "system", Op: "install", Args: "php/8.4", Status: "success"}
	b, err := AuditLine(op)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		t.Fatalf("AuditLine 不应含换行: %q", b)
	}
}
