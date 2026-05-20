-- +goose Up
-- +goose StatementBegin

-- Comments inherit visibility from the node they were posted on.
-- CreateCommentNode copies the parent's visibility at creation time, but
-- nothing keeps the two in sync afterwards: editing the parent's
-- visibility used to leave its comments behind. This trigger closes that
-- gap by mirroring (visibility, visibility_group_id, group_id) changes
-- onto every comment hanging off the updated node via comments_on edges.
-- One- and two-hop walks (top-level comments + replies) are both
-- covered; the recursion bottoms out because comments themselves
-- short-circuit early in the trigger function.

CREATE FUNCTION propagate_visibility_to_comment_descendants() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.type = 'comment' THEN
    RETURN NEW;
  END IF;
  IF NEW.visibility = OLD.visibility
     AND NEW.visibility_group_id IS NOT DISTINCT FROM OLD.visibility_group_id
     AND NEW.group_id IS NOT DISTINCT FROM OLD.group_id THEN
    RETURN NEW;
  END IF;

  WITH RECURSIVE descendants AS (
    SELECT c.id
    FROM nodes c
    JOIN edges e ON e.from_node = c.id
                AND e.kind = 'comments_on'
                AND e.to_node = NEW.id
    WHERE c.type = 'comment'
    UNION
    SELECT c.id
    FROM nodes c
    JOIN edges e ON e.from_node = c.id
                AND e.kind = 'comments_on'
    JOIN descendants d ON e.to_node = d.id
    WHERE c.type = 'comment'
  )
  UPDATE nodes
  SET visibility = NEW.visibility,
      visibility_group_id = NEW.visibility_group_id,
      group_id = NEW.group_id
  WHERE id IN (SELECT id FROM descendants);

  RETURN NEW;
END;
$$;

CREATE TRIGGER nodes_propagate_visibility_to_comments
AFTER UPDATE OF visibility, visibility_group_id, group_id ON nodes
FOR EACH ROW
EXECUTE FUNCTION propagate_visibility_to_comment_descendants();

-- Backfill. Walks every existing comment up the comments_on chain to its
-- root non-comment node and resets the comment's visibility fields to
-- whatever that root currently carries. Drift introduced before the
-- trigger existed is wiped here. Cap the walk at four hops so a
-- hypothetical cycle (the application enforces one-level replies, so
-- two is the real ceiling) cannot loop the CTE forever.
WITH RECURSIVE comment_target AS (
  SELECT c.id AS comment_id, e.to_node AS target_id, 1 AS hop
  FROM nodes c
  JOIN edges e ON e.from_node = c.id AND e.kind = 'comments_on'
  WHERE c.type = 'comment'
  UNION ALL
  SELECT ct.comment_id, e.to_node, ct.hop + 1
  FROM comment_target ct
  JOIN edges e ON e.from_node = ct.target_id AND e.kind = 'comments_on'
  WHERE ct.hop < 4
),
roots AS (
  SELECT DISTINCT ON (ct.comment_id) ct.comment_id, n.id AS root_id
  FROM comment_target ct
  JOIN nodes n ON n.id = ct.target_id
  WHERE n.type <> 'comment'
  ORDER BY ct.comment_id, ct.hop DESC
)
UPDATE nodes c
SET visibility = r_node.visibility,
    visibility_group_id = r_node.visibility_group_id,
    group_id = r_node.group_id
FROM roots r
JOIN nodes r_node ON r_node.id = r.root_id
WHERE c.id = r.comment_id
  AND (
    c.visibility IS DISTINCT FROM r_node.visibility
    OR c.visibility_group_id IS DISTINCT FROM r_node.visibility_group_id
    OR c.group_id IS DISTINCT FROM r_node.group_id
  );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS nodes_propagate_visibility_to_comments ON nodes;
DROP FUNCTION IF EXISTS propagate_visibility_to_comment_descendants();

-- +goose StatementEnd
