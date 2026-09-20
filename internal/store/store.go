// Store 门面：SQLite 唯一持久层入口（modernc 纯 Go 驱动，CGO_ENABLED=0）
// 仅承载运行态（installed/running/sites/php_extensions/dir_ready/trash/operations）；
// 配置真相（路径/密码/端口）不在此，由注入的 EnvProvider（*config.ConfigStore）合成进快照。
package store

import "database/sql"

// sqlNoRows 集中引用，避免各文件重复导入
var sqlNoRows = sql.ErrNoRows

// EnvProvider 提供扁平 env 映射（快照 env 的唯一来源；*config.ConfigStore 满足）
type EnvProvider interface {
	FlatEnv() map[string]string
}

type Store struct {
	db  *sql.DB
	env EnvProvider // 快照 env 来源；未注入时快照 env 为空（首启前安全）
}

// SetEnvProvider 注入配置真相源（DI 在 LoadConfigStore 之后调用）
func (s *Store) SetEnvProvider(p EnvProvider) { s.env = p }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
