-- 一次性回收 bundle_run_events 的历史膨胀。
--
-- 背景：逐 token 的增量以前是一条 delta 一行，一次运行能写上千行
-- {"text":"可"}。从 2026-09-07 起这些增量不再落库（见
-- internal/runstream/store.go），但已有的数据还在表里。
--
-- 这个脚本只删**可证明对回放没有贡献**的行，判据有两条，缺一不可：
--
--   1. 只删 node.thinking。它是答案正文的逐字前缀，而前端 timeline.ts 对
--      node.finished 是赋值（rawTextByNode[node] = text）不是追加，所以有
--      了完整正文之后这些增量一个字都不贡献。
--      **node.reasoning 不在删除范围内**——思维链不会被 node.finished 覆
--      盖（timeline.ts 把它累加进独立字段，还有专门的测试守着），删了就
--      真的丢历史。旧数据里它保持原样，新数据从源头就已经合并成一条。
--
--   2. 该节点必须已经有 node.finished。失败、超时、被取消的运行没有它，
--      那时增量是仅存的部分答案，同样删了就丢历史。
--
-- 用法：
--   psql "$DATABASE_URL" -f scripts/compact_run_events.sql
--
-- 建议先干跑一遍看看会删多少（把最后的 COMMIT 换成 ROLLBACK）。

BEGIN;

SELECT count(*) AS "删除前总行数" FROM bundle_run_events;

WITH deleted AS (
    DELETE FROM bundle_run_events e
    WHERE e.type = 'node.thinking'   -- 见上：reasoning 不能删
      AND EXISTS (
          -- 该节点确实跑完了：完整正文在它的 node.finished 里
          SELECT 1 FROM bundle_run_events f
          WHERE f.run_id = e.run_id
            AND f.node   = e.node
            AND f.type   = 'node.finished'
      )
    RETURNING 1
)
SELECT count(*) AS "本次删除" FROM deleted;

SELECT count(*) AS "删除后总行数" FROM bundle_run_events;

-- 死元组只是标记为不可见，空间要 VACUUM 才真的还给操作系统。
-- VACUUM 不能在事务里跑，所以提交之后单独执行：
--   VACUUM (ANALYZE) bundle_run_events;
-- 想立刻把文件缩小（会锁表，挑低峰期）：
--   VACUUM FULL bundle_run_events;

COMMIT;
