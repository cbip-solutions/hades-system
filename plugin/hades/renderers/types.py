# SPDX-License-Identifier: MIT
# plugin/hades/renderers/types.py
"""Python type stubs mirroring HADES design release track Go envelope substrate."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Any, ClassVar

# Opaque ID type aliases — runtime form is ``str`` but type-checked
# distinctively at API boundaries.
CitationID = str  # opaque ID assigned by HADES design release track augment/pipeline.go
SessionID = str  # Hermes session ID (opaque)
RequestID = str  # daemon request ID (opaque)
AuditEventID = str  # HADES design Tessera event ID (opaque, format "evt-<hex>")

# Doctrine values per HADES design doctrine.toml schema (max-scope/default/capa-firewall).
ALLOWED_DOCTRINES: frozenset[str] = frozenset({"max-scope", "default", "capa-firewall"})


class CitationType(str, Enum):
    """Citation type discriminating payload semantics.

    Mirrors HADES design release track ``internal/citation/types.go`` ``CitationType`` Go
    enum. Adding a value here REQUIRES adding to Go source first
    (cross-language source-of-truth: Go).
    """

    KG_NODE = "kg_node"
    KG_EDGE = "kg_edge"
    FILE_SLICE = "file_slice"
    COMMIT_REF = "commit_ref"
    COMMUNITY_SUMMARY = "community_summary"
    AUDIT_EVENT = "audit_event"
    CUSTOM = "custom"


class CitationSource(str, Enum):
    """Originating retrieval surface for a citation.

    Mirrors HADES design release track ``internal/citation/types.go`` ``CitationSource``.
    HADES design release track renamed the code-graph sources gitnexus_* → caronte_*;
    the old values are kept as backward-compat aliases so pre-HADES design audit
    rows (wire value ``gitnexus_query`` / ``gitnexus_context``) still round-trip
    without error — mirrors Go ``ParseCitationSource`` alias table.
    """

    CARONTE_QUERY = "caronte_query"  # Lane 1 — KG semantic (was GITNEXUS_QUERY)
    CARONTE_CONTEXT = (
        "caronte_context"  # Lane 3 — community + neighbors (was GITNEXUS_CONTEXT)
    )
    AGGREGATOR_FTS = "aggregator_fts"
    AGGREGATOR_VEC = "aggregator_vec"
    TEMPORAL = "temporal"
    MANUAL_OVERRIDE = "manual_override"

    # Backward-compat aliases for pre-HADES design audit rows (wire value preserved).
    GITNEXUS_QUERY = "gitnexus_query"
    GITNEXUS_CONTEXT = "gitnexus_context"


class RetrievalLane(str, Enum):
    """RRF lane that surfaced a citation.

    Mirrors HADES design release track ``internal/citation/types.go`` ``RetrievalLane``.
    """

    SEMANTIC = "semantic"
    LEXICAL = "lexical"
    GRAPH = "graph"
    RERANK = "rerank"
    TEMPORAL = "temporal"


class Platform(str, Enum):
    """Render target.

    6 platform-specific renderers (HADES design release track) + 1 fallback (HADES design
    substrate ``internal/citation/markdown_fallback.go``).
    """

    INK = "ink"
    TELEGRAM = "telegram"
    SLACK = "slack"
    EMAIL = "email"
    VOICE = "voice"
    WEB = "web"
    MARKDOWN_FALLBACK = "markdown_fallback"


@dataclass(frozen=True, slots=True)
class Envelope:
    """Per-citation structured envelope.

    Round-trips with HADES design Go ``internal/citation/types.go`` ``Envelope``
    struct byte-exact via JSON tags. Field order + names MUST match Go json
    tags; validation enforces invariants (confidence in [0.0, 1.0]; id
    non-empty) — these are invariant anchors.

    The Go ``Lane`` field has JSON tag ``retrieval_lane``; Python uses the
    same JSON name (``retrieval_lane`` field on the dataclass).
    """

    id: CitationID
    type: CitationType
    source: CitationSource
    retrieval_lane: RetrievalLane
    audit_event_id: AuditEventID
    confidence: float
    rrf_score: float
    rrf_rank: int
    project_id: str
    payload: str
    expiration: datetime | None = None
    platform_renders: dict[str, dict[str, Any]] = field(default_factory=dict)

    SCHEMA_VERSION: ClassVar[str] = "1.0"

    def __post_init__(self) -> None:
        if not self.id:
            raise ValueError("Envelope.id must be non-empty")
        if not self.type:
            raise ValueError("Envelope.type must be set")
        if not self.source:
            raise ValueError("Envelope.source must be set")
        if not self.retrieval_lane:
            raise ValueError("Envelope.retrieval_lane must be set")
        if not self.audit_event_id:
            raise ValueError("Envelope.audit_event_id must be non-empty")
        if not 0.0 <= self.confidence <= 1.0:
            raise ValueError(
                f"Envelope.confidence must be in [0.0, 1.0]; got {self.confidence!r}"
            )
        if self.rrf_score < 0:
            raise ValueError(f"Envelope.rrf_score must be >= 0; got {self.rrf_score}")
        if self.rrf_rank < -1:
            raise ValueError(
                f"Envelope.rrf_rank < -1 (use -1 for 'not in top-K'); got {self.rrf_rank}"
            )
        if not self.project_id:
            raise ValueError("Envelope.project_id must be non-empty")
        if not self.payload:
            raise ValueError("Envelope.payload must be non-empty")
        if self.expiration is not None and (
            self.expiration.tzinfo is None
            or self.expiration.utcoffset() != timezone.utc.utcoffset(None)
        ):
            raise ValueError(
                "Envelope.expiration must be UTC tz-aware when set; "
                f"got tzinfo={self.expiration.tzinfo!r}"
            )

    def audit_event_url(self) -> str:
        """Canonical hades://audit/<id> deep-link (mirrors Go AuditEventURL)."""
        return f"hades://audit/{self.audit_event_id}"

    def to_dict(self) -> dict[str, Any]:
        """Serialize to dict matching Go json tags."""
        d: dict[str, Any] = {
            "id": self.id,
            "type": self.type.value,
            "source": self.source.value,
            "retrieval_lane": self.retrieval_lane.value,
            "audit_event_id": self.audit_event_id,
            "confidence": self.confidence,
            "rrf_score": self.rrf_score,
            "rrf_rank": self.rrf_rank,
            "project_id": self.project_id,
            "payload": self.payload,
        }
        # Match Go's omitempty on Expiration (zero-value Time omitted).
        if self.expiration is not None:
            d["expiration"] = self.expiration.isoformat()
        # Match Go's omitempty on PlatformRenders (nil/empty map omitted).
        if self.platform_renders:
            d["platform_renders"] = {k: dict(v) for k, v in self.platform_renders.items()}
        return d

    def to_json(self) -> str:
        """Emit JSON-wire form matching Go envelope.go MarshalJSON output."""
        return json.dumps(self.to_dict(), sort_keys=False, separators=(",", ":"))

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> Envelope:
        """Deserialize from dict; raises ValueError on missing/invalid fields."""
        try:
            exp_raw = d.get("expiration")
            expiration: datetime | None = None
            if exp_raw:
                expiration = datetime.fromisoformat(exp_raw)
            pr_raw = d.get("platform_renders", {}) or {}
            platform_renders: dict[str, dict[str, Any]] = {
                k: dict(v) for k, v in pr_raw.items()
            }
            return cls(
                id=str(d["id"]),
                type=CitationType(d["type"]),
                source=CitationSource(d["source"]),
                retrieval_lane=RetrievalLane(d["retrieval_lane"]),
                audit_event_id=str(d["audit_event_id"]),
                confidence=float(d["confidence"]),
                rrf_score=float(d["rrf_score"]),
                rrf_rank=int(d["rrf_rank"]),
                project_id=str(d["project_id"]),
                payload=str(d["payload"]),
                expiration=expiration,
                platform_renders=platform_renders,
            )
        except (KeyError, ValueError, TypeError) as exc:
            raise ValueError(f"Envelope.from_dict failed: {exc}") from exc

    @classmethod
    def from_json(cls, raw: str) -> Envelope:
        try:
            d = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ValueError(f"Envelope.from_json: invalid JSON: {exc}") from exc
        if not isinstance(d, dict):
            raise ValueError(
                f"Envelope.from_json: expected object, got {type(d).__name__}"
            )
        return cls.from_dict(d)


@dataclass(frozen=True, slots=True)
class AugmentationResult:
    """Wrapper type for HADES design release track augmentation pipeline output.

    Round-trips with HADES design Go ``internal/augment/types.go``
    ``AugmentationResult`` struct byte-exact via JSON tags.

    Top-level container for an augmentation pipeline run; doctrine field is
    load-bearing for downstream filtering (e.g., capa-firewall hides
    cross-project citations entirely at render time).

    invariant: structured serialization preserves to Tessera audit chain
    via the per-citation envelopes.
    """

    request_id: RequestID
    session_id: SessionID
    doctrine: str
    project_id: str
    citations: list[Envelope]
    emitted_at: datetime
    kg_token_count: int
    cache_key_hash: str
    audit_event_id: AuditEventID
    static_context: str = ""
    volatile_context: str = ""

    SCHEMA_VERSION: ClassVar[str] = "1.0"

    def __post_init__(self) -> None:
        if not self.request_id:
            raise ValueError("AugmentationResult.request_id must be non-empty")
        if self.doctrine not in ALLOWED_DOCTRINES:
            raise ValueError(
                "AugmentationResult.doctrine must be in "
                f"{sorted(ALLOWED_DOCTRINES)}; got {self.doctrine!r}"
            )
        if not self.project_id:
            raise ValueError("AugmentationResult.project_id must be non-empty")
        if self.kg_token_count < 0:
            raise ValueError(
                "AugmentationResult.kg_token_count must be >= 0; "
                f"got {self.kg_token_count}"
            )
        if self.emitted_at.tzinfo is None or (
            self.emitted_at.utcoffset() != timezone.utc.utcoffset(None)
        ):
            raise ValueError(
                "AugmentationResult.emitted_at must be UTC tz-aware; "
                f"got tzinfo={self.emitted_at.tzinfo!r}"
            )

    def to_dict(self) -> dict[str, Any]:
        return {
            "request_id": self.request_id,
            "session_id": self.session_id,
            "doctrine": self.doctrine,
            "project_id": self.project_id,
            "emitted_at": self.emitted_at.isoformat(),
            "citations": [c.to_dict() for c in self.citations],
            "kg_token_count": self.kg_token_count,
            "cache_key_hash": self.cache_key_hash,
            "audit_event_id": self.audit_event_id,
            "static_context": self.static_context,
            "volatile_context": self.volatile_context,
        }

    def to_json(self) -> str:
        """Emit JSON-wire form matching Go AugmentationResult MarshalJSON."""
        return json.dumps(self.to_dict(), sort_keys=False, separators=(",", ":"))

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> AugmentationResult:
        try:
            return cls(
                request_id=str(d["request_id"]),
                session_id=str(d["session_id"]),
                doctrine=str(d["doctrine"]),
                project_id=str(d["project_id"]),
                citations=[Envelope.from_dict(c) for c in d["citations"]],
                emitted_at=datetime.fromisoformat(d["emitted_at"]),
                kg_token_count=int(d["kg_token_count"]),
                cache_key_hash=str(d["cache_key_hash"]),
                audit_event_id=str(d["audit_event_id"]),
                static_context=str(d.get("static_context", "")),
                volatile_context=str(d.get("volatile_context", "")),
            )
        except (KeyError, ValueError, TypeError) as exc:
            raise ValueError(f"AugmentationResult.from_dict failed: {exc}") from exc

    @classmethod
    def from_json(cls, raw: str) -> AugmentationResult:
        try:
            d = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ValueError(
                f"AugmentationResult.from_json: invalid JSON: {exc}"
            ) from exc
        if not isinstance(d, dict):
            raise ValueError(
                f"AugmentationResult.from_json: expected object, got {type(d).__name__}"
            )
        return cls.from_dict(d)


@dataclass(slots=True)
class RenderResult:
    """Output of a renderer.render() call.

    ``output`` is platform-native:
    - ``str`` for ink (JSON-encoded component tree)/voice/markdown_fallback
    - ``dict[str, Any]`` for slack chat.postMessage payload + ink raw dict
    - ``list[dict[str, Any]]`` for telegram (per-message chunks within
      4096-char limit)
    - ``str`` (HTML) for email/web

    ``metadata`` carries platform-specific signals (e.g., telegram
    inline_keyboard, slack blocks, request_id propagation).
    ``audit_event_ids`` list per-citation event IDs emitted (one per
    citation rendered; empty list when result wraps zero citations).

    NOTE: not frozen (downstream tests + renderer chains may amend
    metadata defensively); default factories independent per instance.
    """

    platform: Platform
    output: Any
    metadata: dict[str, Any] = field(default_factory=dict)
    audit_event_ids: list[AuditEventID] = field(default_factory=list)
