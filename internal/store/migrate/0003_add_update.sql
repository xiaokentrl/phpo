-- 0003：升级检查状态（上次检查时间 / 待安装版本等 KV）
CREATE TABLE update_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- down
DROP TABLE IF EXISTS update_state;
