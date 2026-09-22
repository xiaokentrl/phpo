// 快照读写：状态表 ⇄ model.Snapshot 的完整物化（后端唯一权威的持久形态）
// dirReady 不落库：由 EnvProvider.RootsReady（config.yaml 两根 + 目录存在性）派生进快照。
// sites 的 health/hosts 同样不落库：由快照出口按宿主真值派生（见 enrichSites）。
package store

import (
	"os"
	"path/filepath"

	"phpo/internal/model"
)

func (s *Store) BuildSnapshot() (*model.Snapshot, error) {
	snap := model.NewSnapshot()
	// 任务队列详情在运行态库之前给出：首启未建库时也要能显示「正在跑的第一个任务」
	if s.board != nil {
		snap.Tasks = s.board()
	}
	if s.env != nil {
		snap.Env = s.env.FlatEnv() // 配置真相来自 ConfigStore（YAML），SQLite 不再持有 env 表
		home, www := s.env.RootsReady()
		snap.DirReady = map[string]bool{"PHPO_HOME": home, "WWW_ROOT": www}
	}
	if !s.mayOpen() {
		normalizeCollections(snap)
		return snap, nil // 工作目录未设置：运行态定义为空，且不得建库（首启不在用户数据目录留文件）
	}
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	snap.Installed = map[string][]string{}
	snap.Running = map[string][]string{}
	rows, err := db.Query(`SELECT kind, version, running FROM installed ORDER BY rowid`)
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
	s.enrichSites(snap)
	snap.PHPExtensions = map[string][]string{}
	extRows, err := db.Query(`SELECT version, ext FROM php_extensions ORDER BY rowid`)
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
	normalizeCollections(snap)
	return snap, nil
}

// normalizeCollections 是快照的形状契约：可空集合一律给空集合，不得把 null 交给前端。
// 前端 applySnapshot 是直接展开这些字段的，null 即抛错，抛出即整条 state:changed 落地链中断——
// 队列详情（TaskBrief.Label）永不落地，抽屉右栏只能退化成任务 ID（§5.6.2「事件发了界面却不动」）。
// 零站点时 ListSites 返回 nil、无排队项时队列 Pending 为 nil，两条都会命中这里。
func normalizeCollections(snap *model.Snapshot) {
	if snap.Sites == nil {
		snap.Sites = []model.Site{}
	}
	if snap.Tasks.Pending == nil {
		snap.Tasks.Pending = []model.TaskBrief{}
	}
}

// env / dirReady 已迁出：配置真相与就绪判定唯一来自 ConfigStore（internal/config，YAML）；SQLite 不再持有 env 表与 dir_ready 表。

// enrichSites 在快照出口逐站点补齐展示用运行态（health/hosts）。
// 硬红线 4：这两列只由后端按宿主真值给出，前端不得自行推断；BuildSnapshot 是全部 state:changed
// 与 GetState 的唯一快照来源，故在此一处落地即处处一致。
func (s *Store) enrichSites(snap *model.Snapshot) {
	if len(snap.Sites) == 0 {
		return
	}
	nginxUp := len(snap.Running["nginx"]) > 0
	phpUp := snap.Running["php"]
	sitesRoot := snap.Env["NGINX_SITES_ROOT"]
	for i := range snap.Sites {
		st := &snap.Sites[i]
		st.Hosts = s.hosts != nil && s.hosts(st.Domain)
		st.Health = model.SiteRuntime{
			NginxRunning: nginxUp,
			VHostOnDisk:  sitesRoot != "" && pathExists(filepath.Join(sitesRoot, st.Domain+".conf")),
			PHPRunning:   st.PHP != "" && contains(phpUp, st.PHP),
			RootExists:   st.Root != "" && pathExists(st.Root),
		}.Health()
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// pathExists 路径存在即为真（读错误按不存在处理：宁可显示降级，也不虚报正常）
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// sites
func (s *Store) ListSites() ([]model.Site, error) {
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT domain, port, php, root, rewrite, rewrite_rule, vhost_customized FROM sites ORDER BY domain`)
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
	db, err := s.ensure()
	if err != nil {
		return err
	}
	c := 0
	if st.VhostCustomized {
		c = 1
	}
	_, err = db.Exec(`INSERT INTO sites(domain,port,php,root,rewrite,rewrite_rule,vhost_customized) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(domain) DO UPDATE SET port=excluded.port, php=excluded.php, root=excluded.root, rewrite=excluded.rewrite,
			rewrite_rule=excluded.rewrite_rule, vhost_customized=excluded.vhost_customized`,
		st.Domain, st.Port, st.PHP, st.Root, st.Rewrite, st.RewriteRule, c)
	return err
}

func (s *Store) DeleteSite(domain string) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM sites WHERE domain=?`, domain)
	return err
}

// installed / running
func (s *Store) SetInstalled(kind, version string, installed bool) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	if !installed {
		_, err := db.Exec(`DELETE FROM installed WHERE kind=? AND version=?`, kind, version)
		return err
	}
	_, err = db.Exec(`INSERT INTO installed(kind,version) VALUES(?,?) ON CONFLICT(kind,version) DO NOTHING`, kind, version)
	return err
}

func (s *Store) SetRunning(kind, version string, running bool) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	n := 0
	if running {
		n = 1
	}
	_, err = db.Exec(`UPDATE installed SET running=? WHERE kind=? AND version=?`, n, kind, version)
	return err
}

// PHP 扩展（整组替换，幂等）
func (s *Store) SetPHPExtensions(version string, exts []string) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
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
