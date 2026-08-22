DROP TRIGGER IF EXISTS resources_reject_immutable_delete ON resources;
DROP TRIGGER IF EXISTS resources_reject_immutable_update ON resources;
DROP TRIGGER IF EXISTS bundles_reject_immutable_delete ON bundles;
DROP TRIGGER IF EXISTS bundles_reject_immutable_update ON bundles;
DROP TRIGGER IF EXISTS agents_reject_immutable_delete ON agents;
DROP TRIGGER IF EXISTS agents_reject_immutable_update ON agents;

DROP FUNCTION IF EXISTS reject_immutable_delete();
DROP FUNCTION IF EXISTS reject_immutable_update();
