ALTER TABLE inventory_movements DROP COLUMN IF EXISTS batch_id;
DROP TABLE IF EXISTS inventory_batches;
ALTER TABLE products DROP COLUMN IF EXISTS shelf_life_days;
