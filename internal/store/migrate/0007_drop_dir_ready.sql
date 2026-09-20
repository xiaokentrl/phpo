-- 0007：dirReady 下线——就绪判定唯一派生自 config.yaml 两根 + 目录存在性（§4.2 / §5.6）
-- 原 dir_ready 表是每次启动都被 RefreshDirReady 覆盖的派生缓存，删除不丢任何信息。
DROP TABLE IF EXISTS dir_ready;

-- down
CREATE TABLE dir_ready (
  key   TEXT PRIMARY KEY,
  ready INTEGER NOT NULL
);
