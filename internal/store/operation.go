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
	_, err = db.Exec(`INSERT INTO operations(ts,actor,op,args,status,duration_ms,error) VALUES(?,?,?,?,?,?,?)`,
		op.TS.Format(time.RFC3339Nano), op.Actor, op.Op, string(args), op.Status, op.DurationMs, op.Error)
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
	rows, err := db.Query(`SELECT ts,actor,op,args,status,duration_ms,error FROM operations ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Operation
	for rows.Next() {
		var (
			op     model.Operation
			ts, a  string
			errStr *string
		)
		if err := rows.Scan(&ts, &op.Actor, &op.Op, &a, &op.Status, &op.DurationMs, &errStr); err != nil {
			return nil, err
		}
		op.TS, _ = time.Parse(time.RFC3339Nano, ts)
		_ = json.Unmarshal([]byte(a), &op.Args)
		if errStr != nil {
			op.Error = *errStr
		}
		out = append(out, op)
	}
	return out, nil
}
