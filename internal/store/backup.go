// SQLite 一致性快照（T602 备份底座）：用 VACUUM INTO 生成独立只读副本，不锁主库、不含 WAL 残留。
package store

import "fmt"

// BackupTo 把当前库快照写入 dst（目标文件必须不存在，VACUUM INTO 语义）；供备份归档打包。
func (s *Store) BackupTo(dst string) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("生成 SQLite 快照失败: %w", err)
	}
	return nil
}
