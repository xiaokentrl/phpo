-- 0004：操作审计（operations.log 为 JSON Lines 权威文件，此表供 UI 历史查询）
CREATE TABLE operations (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ts          TEXT    NOT NULL,
  actor       TEXT    NOT NULL,
  op          TEXT    NOT NULL,
  args        TEXT    NOT NULL,
  status      TEXT    NOT NULL,
  duration_ms INTEGER NOT NULL,
  error       TEXT
);

-- down
DROP TABLE IF EXISTS operations;
