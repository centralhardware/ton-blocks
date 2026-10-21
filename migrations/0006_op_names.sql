-- names for op codes; the logger re-inserts op_names*.tsv on every start and
-- ReplacingMergeTree(updated_at) keeps the newest row per op.
CREATE TABLE IF NOT EXISTS ton.op_names (
    op               Int64,
    name             String,
    protocol         LowCardinality(String),
    description      String,
    source           String,
    updated_at       DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY op;
