CREATE OR REPLACE FUNCTION reject_immutable_update() RETURNS trigger AS $$
BEGIN
  IF OLD.immutable THEN
    RAISE EXCEPTION '该版本已被订阅，不可修改（快照隔离）';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_immutable_delete() RETURNS trigger AS $$
BEGIN
  IF OLD.immutable THEN
    RAISE EXCEPTION '该版本已被订阅，不可删除（快照隔离）';
  END IF;
  RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER agents_reject_immutable_update
  BEFORE UPDATE ON agents
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER agents_reject_immutable_delete
  BEFORE DELETE ON agents
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

CREATE TRIGGER bundles_reject_immutable_update
  BEFORE UPDATE ON bundles
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER bundles_reject_immutable_delete
  BEFORE DELETE ON bundles
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

CREATE TRIGGER resources_reject_immutable_update
  BEFORE UPDATE ON resources
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER resources_reject_immutable_delete
  BEFORE DELETE ON resources
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();
