-- 0008：任务账本——每个三段式任务的终态连同等效日志落 operations，供 UI「历史任务」实时回看成功/失败原因
ALTER TABLE operations ADD COLUMN task_id TEXT;
ALTER TABLE operations ADD COLUMN label TEXT;
ALTER TABLE operations ADD COLUMN logs TEXT;

-- down
ALTER TABLE operations DROP COLUMN task_id;
ALTER TABLE operations DROP COLUMN label;
ALTER TABLE operations DROP COLUMN logs;
