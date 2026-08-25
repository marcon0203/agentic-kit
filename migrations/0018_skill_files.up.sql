-- A Skill uploaded as a zip (spec-05a) is a file tree, not a single string
-- — this indexes what's actually in it (path/size/type), so the list page
-- can show and let the user download individual files. The file bytes
-- themselves live in Aliyun OSS, keyed by skills.config->>'oss_prefix';
-- this table is purely an index, never the content.
CREATE TABLE skill_files (
    id           BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    skill_id     BIGINT        NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    owner_user_id BIGINT       NOT NULL REFERENCES users(id),
    path         VARCHAR(500)  NOT NULL,
    size_bytes   BIGINT        NOT NULL,
    content_type VARCHAR(100)  NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (skill_id, path)
);

CREATE INDEX skill_files_skill_id_idx ON skill_files (skill_id, owner_user_id);
