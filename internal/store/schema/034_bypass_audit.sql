-- Migration 034: extend bypass_audit with conversation_id (HADES design release track).
-- Additive only; no data loss. Column is nullable because pre-release track rows
-- didn't carry a conversation grouping.
ALTER TABLE bypass_audit ADD COLUMN conversation_id TEXT;
CREATE INDEX IF NOT EXISTS idx_bypass_audit_conversation
    ON bypass_audit(conversation_id);
