// SQLite 打开与 PRAGMA：WAL + busy_timeout + 外键（§2 持久化选型）
// Open 立即建库并迁移（备份恢复读归档内快照、单测与集成测试用）；
// 应用自身数据库用 New 延迟打开（见 store.go），使首启未设置工作目录时不创建用户数据目录。
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open 立即打开（或创建）phpo.db 并完成迁移
func Open(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{path: path, db: db}, nil
}

// openDB 建父目录 → 打开连接：modernc 驱动下串行写入避免 WAL 竞争
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	dsn := "file:" + url.PathEscape(filepath.ToSlash(path)) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
