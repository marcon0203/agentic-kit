ALTER TABLE mcp_sources
    DROP COLUMN IF EXISTS api_key_encrypted,
    DROP COLUMN IF EXISTS api_prefix,
    DROP COLUMN IF EXISTS protocol;
