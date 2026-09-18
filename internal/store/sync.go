// applyStateChange 落库助手：任务成功后按 meta 语义写状态表（唯一合法写路径）
package store

import (
	"database/sql"

	"phpo/internal/model"
)

// ApplyTaskResult 由 task 层在事务提交点调用；禁止 UI 旁路（硬红线 5）
func (s *Store) ApplyTaskResult(meta model.TaskMeta, snap *model.Snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 全量物化当前快照：installed / sites / env / ext / dirReady
	if _, err := tx.Exec(`DELETE FROM installed`); err != nil {
		return err
	}
	for kind, versions := range snap.Installed {
		for _, v := range versions {
			running := 0
			for _, r := range snap.Running[kind] {
				if r == v {
					running = 1
					break
				}
			}
			if _, err := tx.Exec(`INSERT INTO installed(kind,version,running) VALUES(?,?,?)`, kind, v, running); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM sites`); err != nil {
		return err
	}
	for _, st := range snap.Sites {
		if _, err := tx.Exec(`INSERT INTO sites(domain,port,php,root,rewrite) VALUES(?,?,?,?,?)`,
			st.Domain, st.Port, st.PHP, st.Root, st.Rewrite); err != nil {
			return err
		}
	}
	if err := replaceKV(tx, `env`, snap.Env); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM php_extensions`); err != nil {
		return err
	}
	for ver, exts := range snap.PHPExtensions {
		for _, e := range exts {
			if _, err := tx.Exec(`INSERT INTO php_extensions(version,ext) VALUES(?,?)`, ver, e); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM dir_ready`); err != nil {
		return err
	}
	for k, r := range snap.DirReady {
		n := 0
		if r {
			n = 1
		}
		if _, err := tx.Exec(`INSERT INTO dir_ready(key,ready) VALUES(?,?)`, k, n); err != nil {
			return err
		}
	}
	_ = meta // meta 的语义已由调用方反映进 snap；保留参数以便审计关联
	return tx.Commit()
}

func replaceKV(tx *sql.Tx, table string, kv map[string]string) error {
	if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
		return err
	}
	for k, v := range kv {
		if _, err := tx.Exec(`INSERT INTO `+table+`(key,value) VALUES(?,?)`, k, v); err != nil {
			return err
		}
	}
	return nil
}
