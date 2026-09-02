DROP INDEX IF EXISTS market_skills_review_idx;

ALTER TABLE market_skills
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS review_status;
