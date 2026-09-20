// Store 门面：SQLite 唯一持久层入口（modernc 纯 Go 驱动，CGO_ENABLED=0）
// 仅承载运行态（installed/running/sites/php_extensions/trash/operations/offline/cache_manifest）；
// 配置真相（根目录/密码/端口）与 dirReady 不在此，由注入的 EnvProvider（*config.ConfigStore）合成进快照。
// 延迟打开：New 只记路径，首次访问才「建父目录 → 建库 → 迁移」；工作目录未设置时拒绝打开，
// 使首启不在用户数据目录留下任何文件（装机向导把两根写入 config.yaml 后自然放行）。
package store

import (
	"database/sql"
	"errors"
	"sync"

	"phpo/pkg/errs"
)

// sqlNoRows 集中引用，避免各文件重复导入
var sqlNoRows = sql.ErrNoRows

// ErrHomeNotSet 工作目录未设置时拒绝对运行态存储的一切读写（复用 preflight 的 homeNotReady 文案）。
// 未设置即无任何可安装/可建站的前提，故这不是功能限制，而是防止在用户数据目录留下半成品文件。
var ErrHomeNotSet = errors.New(errs.HomeNotReady)

// EnvProvider 提供配置真相（快照 env 与 dirReady 的唯一来源；*config.ConfigStore 满足）
type EnvProvider interface {
	FlatEnv() map[string]string
	RootsPersisted() bool         // 两根是否已写入 config.yaml → 决定能否创建/打开 phpo.db
	RootsReady() (home, www bool) // 两根是否「已持久化且目录存在」→ 派生快照 dirReady
}

type Store struct {
	mu    sync.Mutex // 保护 db 的一次性打开（多任务并发访问）
	path  string     // phpo.db 绝对路径（延迟打开用）
	db    *sql.DB    // nil 表示尚未打开
	env   EnvProvider
	hosts HostsProbe
}

// New 返回延迟打开的运行态存储：不建目录、不建库、不迁移。装配期用它注入各服务门面。
func New(path string) *Store { return &Store{path: path} }

// SetEnvProvider 注入配置真相源（装配期一次性调用，先于任何并发访问）
func (s *Store) SetEnvProvider(p EnvProvider) { s.env = p }

// HostsProbe 查询域名是否已被系统 hosts 解析到 127.0.0.1（快照 sites.hosts 的唯一来源）。
// store 不得反向依赖 vhost/hosts 包，故由装配层注入实现（*hosts.Manager.Has）。未注入按未解析处理。
type HostsProbe func(domain string) bool

// SetHostsProbe 注入 hosts 探针（装配期一次性调用）
func (s *Store) SetHostsProbe(p HostsProbe) { s.hosts = p }

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil // 从未打开：首启退出时不落任何文件
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// mayOpen 报告是否允许建库：未注入 provider（单测、归档快照只读）视为允许
func (s *Store) mayOpen() bool { return s.env == nil || s.env.RootsPersisted() }

// ensure 首次访问时完成「建父目录 → 打开 → 迁移」，其后幂等返回同一连接；
// 工作目录未持久化时返回 ErrHomeNotSet，绝不创建用户数据目录。
func (s *Store) ensure() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	if !s.mayOpen() {
		return nil, ErrHomeNotSet
	}
	db, err := openDB(s.path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	s.db = db
	return db, nil
}
