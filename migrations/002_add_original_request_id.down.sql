-- Remove original_request_id column from permission_requests

DROP INDEX IF EXISTS idx_permission_requests_original_id;
ALTER TABLE permission_requests DROP COLUMN IF EXISTS original_request_id;
