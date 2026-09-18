// 快照读写：状态表 ⇄ model.Snapshot 的完整物化（后端唯一权威的持久形态）
package store

import (
	"phpo/internal/model"
)

func (s *Store) BuildSnapshot() (*model.Snapshot, error) {
	snap := model.NewSnapshot()
	var err error
	if snap.Env, err = s.AllEnv(); err != nil {
		return nil, err
	}
	snap.Installed = map[string][]string{}
	snap.Running = map[string][]string{}
	rows, err := s.db.Query(`SELECT kind, version, running FROM installed ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, version string
		var running int
		if err := rows.Scan(&kind, &version, &running); err != nil {
			return nil, err
		}
		snap.Installed[kind] = append(snap.Installed[kind], version)
		if running == 1 {
			snap.Running[kind] = append(snap.Running[kind], version)
		}
	}
	if snap.Sites, err = s.ListSites(); err != nil {
		return nil, err
	}
	snap.PHPExtensions = map[string][]string{}
	extRows, err := s.db.Query(`SELECT version, ext FROM php_extensions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer extRows.Close()
	for extRows.Next() {
		var v, e string
		if err := extRows.Scan(&v, &e); err != nil {
			return nil, err
		}
		snap.PHPExtensions[v] = append(snap.PHPExtensions[v], e)
	}
	snap.DirReady = map[string]bool{}
	drRows, err := s.db.Query(`SELECT key, ready FROM dir_ready`)
	if err != nil {
		return nil, err
	}
	defer drRows.Close()
	for drRows.Next() {
		var k string
		var r int
		if err := drRows.Scan(&k, &r); err != nil {
			return nil, err
		}
		snap.DirReady[k] = r == 1
	}
	return snap, nil
}

// env CRUD
func (s *Store) AllEnv() (map[string]string, error) {
	out := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM env`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func (s *Store) GetEnv(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM env WHERE key=?`, key).Scan(&v)
	if err == sqlNoRows {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetEnv 明文写入（值可为空串——空密码合法）
func (s *Store) SetEnv(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO env(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) SetDirReady(key string, ready bool) error {
	n := 0
	if ready {
		n = 1
	}
	_, err := s.db.Exec(`INSERT INTO dir_ready(key,ready) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET ready=excluded.ready`, key, n)
	return err
}

// sites
func (s *Store) ListSites() ([]model.Site, error) {
	rows, err := s.db.Query(`SELECT domain, port, php, root, rewrite, rewrite_rule, vhost_customized FROM sites ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Site
	for rows.Next() {
		var st model.Site
		var customized int
		if err := rows.Scan(&st.Domain, &st.Port, &st.PHP, &st.Root, &st.Rewrite, &st.RewriteRule, &customized); err != nil {
			return nil, err
		}
		st.VhostCustomized = customized != 0
		out = append(out, st)
	}
	return out, nil
}

func (s *Store) UpsertSite(st model.Site) error {
	c := 0
	if st.VhostCustomized {
		c = 1
	}
	_, err := s.db.Exec(`INSERT INTO sites(domain,port,php,root,rewrite,rewrite_rule,vhost_customized) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(domain) DO UPDATE SET port=excluded.port, php=excluded.php, root=excluded.root, rewrite=excluded.rewrite,
			rewrite_rule=excluded.rewrite_rule, vhost_customized=excluded.vhost_customized`,
		st.Domain, st.Port, st.PHP, st.Root, st.Rewrite, st.RewriteRule, c)
	return err
}

func (s *Store) DeleteSite(domain string) error {
	_, err := s.db.Exec(`DELETE FROM sites WHERE domain=?`, domain)
	return err
}

// installed / running
func (s *Store) SetInstalled(kind, version string, installed bool) error {
	if !installed {
		_, err := s.db.Exec(`DELETE FROM installed WHERE kind=? AND version=?`, kind, version)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO installed(kind,version) VALUES(?,?) ON CONFLICT(kind,version) DO NOTHING`, kind, version)
	return err
}

func (s *Store) SetRunning(kind, version string, running bool) error {
	n := 0
	if running {
		n = 1
	}
	_, err := s.db.Exec(`UPDATE installed SET running=? WHERE kind=? AND version=?`, n, kind, version)
	return err
}

// PHP 扩展（整组替换，幂等）
func (s *Store) SetPHPExtensions(version string, exts []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM php_extensions WHERE version=?`, version); err != nil {
		tx.Rollback()
		return err
	}
	for _, e := range exts {
		if _, err := tx.Exec(`INSERT INTO php_extensions(version,ext) VALUES(?,?)`, version, e); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
