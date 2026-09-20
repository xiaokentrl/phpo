-- 0001 init：核心状态表（后端唯一权威持久层）
-- 注：配置真相（目录/密码/端口）改由 XDG config.yaml（internal/config.ConfigStore）持有；SQLite 不再设 env 表。
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE installed (
  kind    TEXT NOT NULL,
  version TEXT NOT NULL,
  running INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (kind, version)
);
CREATE TABLE sites (
  domain  TEXT PRIMARY KEY,
  port    INTEGER NOT NULL,
  php     TEXT NOT NULL,
  root    TEXT NOT NULL,
  rewrite TEXT NOT NULL
);
CREATE TABLE php_extensions (
  version TEXT NOT NULL,
  ext     TEXT NOT NULL,
  PRIMARY KEY (version, ext)
);
CREATE TABLE dir_ready (
  key   TEXT PRIMARY KEY,
  ready INTEGER NOT NULL
);
CREATE TABLE trash (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  kind       TEXT NOT NULL,
  orig_path  TEXT NOT NULL,
  trash_path TEXT NOT NULL,
  moved_at   TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

-- down
DROP TABLE IF EXISTS trash;
DROP TABLE IF EXISTS dir_ready;
DROP TABLE IF EXISTS php_extensions;
DROP TABLE IF EXISTS sites;
DROP TABLE IF EXISTS installed;
DROP TABLE IF EXISTS meta;
