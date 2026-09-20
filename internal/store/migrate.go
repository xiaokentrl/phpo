// 迁移执行器：internal/store/migrate/*.sql 按序号应用；`-- down` 段为回滚脚本
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrate/*.sql
var migrationFS embed.FS

// 每个 up 事务后记录版本号；重复执行安全（幂等）
func (s *Store) Migrate() error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	return migrate(db)
}

// migrate 在给定连接上按序号应用未执行的迁移（由 Open / ensure 调用）
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrate")
	if err != nil {
		return err
	}
	versions := make([]int, 0, len(entries))
	files := map[int]string{}
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err != nil {
			return fmt.Errorf("迁移文件名不合规 %s: %w", e.Name(), err)
		}
		versions = append(versions, v)
		files[v] = e.Name()
	}
	sort.Ints(versions)
	for _, v := range versions {
		var has int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, v).Scan(&has); err != nil {
			return err
		}
		if has > 0 {
			continue
		}
		src, err := migrationFS.ReadFile("migrate/" + files[v])
		if err != nil {
			return err
		}
		up, _, ok := cutDown(string(src))
		if !ok {
			return fmt.Errorf("迁移 %s 缺少 -- down 段", files[v])
		}
		if err := execScript(db, up); err != nil {
			return fmt.Errorf("迁移 %s 失败: %w", files[v], err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, v); err != nil {
			return err
		}
	}
	return nil
}

// MigrateDownTo 回滚至目标版本（0 = 全部回滚），按版本逆序执行 down 段
func (s *Store) MigrateDownTo(target int) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	return migrateDownTo(db, target)
}

func migrateDownTo(db *sql.DB, target int) error {
	var versions []int
	rows, err := db.Query(`SELECT version FROM schema_migrations WHERE version > ? ORDER BY version DESC`, target)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, v)
	}
	rows.Close()
	for _, v := range versions {
		name, err := migrationName(v)
		if err != nil {
			return err
		}
		src, err := migrationFS.ReadFile("migrate/" + name)
		if err != nil {
			return err
		}
		_, down, _ := cutDown(string(src))
		if err := execScript(db, down); err != nil {
			return fmt.Errorf("回滚 %s 失败: %w", name, err)
		}
		if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=?`, v); err != nil {
			return err
		}
	}
	return nil
}

func migrationName(v int) (string, error) {
	entries, err := migrationFS.ReadDir("migrate")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		var got int
		if _, err := fmt.Sscanf(e.Name(), "%d_", &got); err == nil && got == v {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("找不到版本 %d 的迁移文件", v)
}

// cutDown 以单独一行 `-- down` 为界拆分
func cutDown(src string) (up, down string, ok bool) {
	const marker = "\n-- down\n"
	if i := strings.Index(src, marker); i >= 0 {
		return src[:i], src[i+len(marker):], true
	}
	return src, "", false
}

// execScript 按分号逐条执行（DDL 场景，忽略空段与注释段）
func execScript(db *sql.DB, script string) error {
	for _, s2 := range strings.Split(script, ";") {
		stmt := strings.TrimSpace(stripComments(s2))
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func stripComments(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}
