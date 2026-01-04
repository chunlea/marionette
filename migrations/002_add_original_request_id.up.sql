-- Add original_request_id column to permission_requests
-- This stores the original request ID from the agent (e.g., Claude's tool_use_id)
-- so we can send the correct ID back when approving/denying the request.

ALTER TABLE permission_requests
    ADD COLUMN original_request_id TEXT NOT NULL DEFAULT '';

-- Create an index for looking up by original request ID if needed
CREATE INDEX idx_permission_requests_original_id ON permission_requests(original_request_id);
