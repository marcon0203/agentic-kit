-- Skill 源同步下来的条目要先过审才对用户可见：一个公开市场里混着大量
-- 低质量条目，全量同步进来会淹没 Skill 管理页。审核状态挂在 market_skills
-- 上而不是新开一张表——它是这条快照自身的属性，一条一行，删源级联清掉。
--
-- 默认 pending：新同步进来的条目一律不可见，管理员批准后才进 Skill 管理
-- 的市场视图。这是"默认拒绝"，不是"默认放行再删"——后者在同步几百条时等
-- 于没有门槛。
ALTER TABLE market_skills
    ADD COLUMN review_status VARCHAR(16) NOT NULL DEFAULT 'pending',  -- pending | approved | rejected
    ADD COLUMN review_note   TEXT,
    ADD COLUMN reviewed_at   TIMESTAMPTZ,
    ADD COLUMN reviewed_by   BIGINT REFERENCES users(id);

-- 用户侧的市场视图只查 approved，审核台按状态筛选，两边都走这个索引。
CREATE INDEX market_skills_review_idx ON market_skills (review_status);
