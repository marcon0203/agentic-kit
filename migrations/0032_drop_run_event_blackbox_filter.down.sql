ALTER TABLE bundle_run_events
    ADD COLUMN is_internal BOOLEAN NOT NULL DEFAULT false;
