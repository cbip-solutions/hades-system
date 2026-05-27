-- Migration 035: encrypted body storage (Q7 D, invariant).
-- Body rows are 1:1 with bypass_audit rows but populated ONLY when the
-- request was served by tier=in-house (invariant). request_body and
-- response_body hold AES-256-GCM ciphertext layout: nonce(12) || ct_with_tag.
-- Key lives in the macOS Keychain (service "anthropic-bypass-bodies-key");
-- key_version supports future rotation (out of HADES design scope).
CREATE TABLE IF NOT EXISTS bypass_audit_bodies (
    audit_id      INTEGER PRIMARY KEY,
    request_body  BLOB    NOT NULL,
    response_body BLOB    NOT NULL,
    encrypted_at  INTEGER NOT NULL,
    key_version   INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (audit_id) REFERENCES bypass_audit(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_bypass_audit_bodies_encrypted_at
    ON bypass_audit_bodies(encrypted_at);
