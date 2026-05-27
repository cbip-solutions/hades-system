-- internal/research/ecosystem/migrations/006_ecosystem_changes.sql
--
-- the release design release track Task A-9. Per spec §3.4.
--
-- VersionRAG Change nodes (per spec §2.5 Q5=A). release track change_extractor
-- writes one row per (package, version_from, version_to, symbol_path)
-- tuple. source_extracted distinguishes explicit_changelog vs
-- implicit_deepdiff per spec §3.3 ChangeNode field set.
--
-- invariant: Change-node graph consistency — every row's
-- (version_from, version_to) MUST correspond to ecosystem_versions rows
-- via FK chain (enforced by trigger; declared at release track E-7).
--
-- CHECK constraints enforce domain enums at the SQL layer per project
-- doctrine "domain invariants load-bearing in 3 places (code +
-- invariants.sql + CHECK constraints)". The Go-side enums in types.go
-- (ChangeType + the source_extracted string-set) are the first place;
-- this CHECK is the second; invariants.sql (release track) is the third.

CREATE TABLE IF NOT EXISTS ecosystem_changes (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id        INTEGER NOT NULL REFERENCES ecosystem_packages(id) ON DELETE CASCADE,
    version_from      TEXT NOT NULL,
    version_to        TEXT NOT NULL,
    change_type       TEXT NOT NULL CHECK (change_type IN ('added','removed','changed','deprecated','moved')),
    symbol_path       TEXT,
    description       TEXT,
    source_extracted  TEXT NOT NULL CHECK (source_extracted IN ('explicit_changelog','implicit_deepdiff','haiku_inferred','operator_annotated')),
    UNIQUE (package_id, version_from, version_to, symbol_path)
);

CREATE INDEX IF NOT EXISTS idx_changes_versions ON ecosystem_changes(package_id, version_from, version_to);
