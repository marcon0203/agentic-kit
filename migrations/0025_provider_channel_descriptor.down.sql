-- 回滚只去掉两列。被删掉的目录数据不恢复：那是 0016 的种子数据加上管理员
-- 自己建的东西，回滚一个删除迁移不该假装能把后者变回来。
ALTER TABLE catalog_providers
    DROP COLUMN IF EXISTS template,
    DROP COLUMN IF EXISTS descriptor;
