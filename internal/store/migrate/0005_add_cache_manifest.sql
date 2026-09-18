-- 0005：缓存清单镜像（manifest.json 原文快照，损坏检测与迁移辅助）
CREATE TABLE cache_manifest (
  kind       TEXT NOT NULL,
  version    TEXT NOT NULL,
  manifest   TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (kind, version)
);

-- down
DROP TABLE IF EXISTS cache_manifest;
