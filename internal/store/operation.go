// 操作审计落表：供 UI 历史查询（JSON Lines 文件权威在 engine/audit.go，M6 接入）
package store

import (
	"encoding/json"
	"time"

	"phpo/internal/model"
)

func (s *Store) AppendOperation(op model.Operation) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	if op.TS.IsZero() {
		op.TS = time.Now().UTC()
	}
	args, err := json.Marshal(op.Args)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO operations(ts,actor,op,args,status,duration_ms,error,task_id,label,logs) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		op.TS.Format(time.RFC3339Nano), op.Actor, op.Op, string(args), op.Status, op.DurationMs, op.Error,
		op.TaskID, op.Label, op.Logs)
	return err
}

// ClearOperations 清空操作账本（任务队列历史 + 任务日志的唯一持久层）：覆盖安装（运行版本变化）
// 后由应用装配调用，新版首屏队列与日志从零开始。只动 operations 这一张表——
// 回收站、清理审计、运行态（installed/running/sites）一律不碰，不属「任务记录」。
func (s *Store) ClearOperations() error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM operations`)
	return err
}

func (s *Store) ListOperations(limit int) ([]model.Operation, error) {
	if limit <= 0 {
		limit = 100
	}
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT ts,actor,op,args,status,duration_ms,error,task_id,label,logs FROM operations ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Operation
	for rows.Next() {
		var (
			op                model.Operation
			ts, a             string
			errStr, idStr     *string
			labelStr, logsStr *string
		)
		if err := rows.Scan(&ts, &op.Actor, &op.Op, &a, &op.Status, &op.DurationMs, &errStr, &idStr, &labelStr, &logsStr); err != nil {
			return nil, err
		}
		op.TS, _ = time.Parse(time.RFC3339Nano, ts)
		_ = json.Unmarshal([]byte(a), &op.Args)
		op.TaskID, op.Label, op.Logs = deref(idStr), deref(labelStr), deref(logsStr)
		op.Error = deref(errStr)
		out = append(out, op)
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
