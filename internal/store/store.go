// Store 门面：SQLite 唯一持久层入口（modernc 纯 Go 驱动，CGO_ENABLED=0）
package store

import "database/sql"

// sqlNoRows 集中引用，避免各文件重复导入
var sqlNoRows = sql.ErrNoRows

type Store struct {
	db *sql.DB
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
