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

	"phpo/internal/util"
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
	normalizeDBPerms(path)
	return &Store{path: path, db: db}, nil
}

// openDB 建父目录 → 打开连接：modernc 驱动下串行写入避免 WAL 竞争
func openDB(path string) (*sql.DB, error) {
	if err := util.MkdirAll(filepath.Dir(path)); err != nil {
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

// normalizeDBPerms 把库文件连同一并存在的 WAL / SHM 伴生文件归一为 0777（§5.20）。
// openDB 只归一了父目录：modernc 驱动建库时走 os 默认的 0666&~umask，umask 022 下即停在 0644。
// chmod 失败 best-effort 忽略（库可能属他人，不该因此判死打开）；伴生文件在干净关闭时会被移除、
// 下次写入时重建，故其权限随每次打开一并归一。
func normalizeDBPerms(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(p, util.FilePerm)
	}
}
