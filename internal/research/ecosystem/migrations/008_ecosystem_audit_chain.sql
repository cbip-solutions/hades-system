-- internal/research/ecosystem/migrations/008_ecosystem_audit_chain.sql
--
-- the release design release track Task A-9. Per spec §3.4 + §4.6.
--
-- the release design RAG audit chain (per ADR-0062 option C: per-event leaf +
-- monthly partition seal hybrid). release track dispatcher writes 8 EventType
-- rows per Query/Ingest cycle (slots 92..99, EvtRAGQuery..EvtRAGIngestJoinKey).
-- partition_id = yyyy-mm; weekly Sunday 03:00 sweep (release track) seals the
-- prior month's partition.
--
-- invariant: seq is monotonic, APPEND-ONLY, no reuse. Enforced by
-- AUTOINCREMENT semantics (sqlite_sequence is never reused after row
-- deletion in this design).
--
-- parent_hash + self_hash = sha256(seq || event_type || payload_json || parent_hash)
-- per spec §4.6.
--
-- CHECK (event_type BETWEEN 92 AND 99) is the load-bearing backstop for
-- the Go-side EvtRAG* constants in internal/orchestrator/eventlog/events.go;
-- per project doctrine "domain invariants load-bearing in 3 places".

CREATE TABLE IF NOT EXISTS ecosystem_audit_chain (
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type    INTEGER NOT NULL CHECK (event_type BETWEEN 92 AND 99),
    payload_json  TEXT NOT NULL,
    parent_hash   TEXT NOT NULL,
    self_hash     TEXT NOT NULL,
    emitted_at    DATETIME NOT NULL,
    doctrine      TEXT NOT NULL,
    partition_id  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_chain_partition ON ecosystem_audit_chain(partition_id);
CREATE INDEX IF NOT EXISTS idx_audit_chain_emitted_at ON ecosystem_audit_chain(emitted_at);
