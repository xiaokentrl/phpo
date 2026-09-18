-- 0006：站点补充伪静态自定义规则与 vhost 手改标记（M4 站点闭环）
ALTER TABLE sites ADD COLUMN rewrite_rule TEXT NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN vhost_customized INTEGER NOT NULL DEFAULT 0;

-- down
ALTER TABLE sites DROP COLUMN vhost_customized;
ALTER TABLE sites DROP COLUMN rewrite_rule;
