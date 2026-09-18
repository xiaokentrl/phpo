-- 0002：离线缓存条目视图（明细以 manifest.json 为权威，此表为加速索引）
CREATE TABLE offline_entries (
  kind        TEXT NOT NULL,
  version     TEXT NOT NULL,
  has_image   INTEGER NOT NULL DEFAULT 0,
  total_size  INTEGER NOT NULL DEFAULT 0,
  last_verify TEXT,
  verify_ok   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (kind, version)
);

-- down
DROP TABLE IF EXISTS offline_entries;
