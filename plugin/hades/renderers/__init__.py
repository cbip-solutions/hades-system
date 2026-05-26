# SPDX-License-Identifier: MIT
                                    
"""HADES citation renderers — 6 platform-specific (Plan 12 Phase A) +"""

from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from datetime import datetime, timezone
from typing import Any

from hermes_plugins.hades.renderers.types import (
    AuditEventID,
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)

__all__ = [
    "AUDIT_EMIT_PATH",
    "DEFAULT_AUDIT_ENDPOINT",
    "DEFAULT_DAEMON_URL",
    "Renderer",
    "RendererRegistry",
    "register_default_renderers",
]

_log = logging.getLogger(__name__)


                                                                          
                                                  
                                                                         
                                                                       
                                                                
                                  
 
                                                               
                                                                    
                                                                 
                                                                    
                                       
                                                        
                                                                    
                                                                  
                                                                 
                                                                      
                                               
 
                                                   
                                                                      
                                                                     
                                                                    
                                  
DEFAULT_DAEMON_URL: str = "http://localhost:4471"

                                                                        
                                                          
AUDIT_EMIT_PATH: str = "/v1/audit/emit"
DEFAULT_AUDIT_ENDPOINT: str = f"{DEFAULT_DAEMON_URL}{AUDIT_EMIT_PATH}"


def _derive_audit_endpoint(daemon_url: str | None) -> str:
    """Derive ``<daemon_url>/v1/audit/emit`` from an optional override.

    None / empty → ``DEFAULT_AUDIT_ENDPOINT``. Trailing slashes on the
    base URL are stripped before composing the path; the resulting URL
    has exactly one ``/v1/audit/emit`` segment.

    Drift note: pre-C-1 fix-cycle this composed a legacy anchor-style
    path that the daemon never registered (``internal/daemon/server.go:737``
    binds the canonical ``AUDIT_EMIT_PATH`` only). Stage 2 cross-phase
    code reviewer surfaced the drift; this helper now matches the
    Phase D AFK contract (``plugin/hades/afk/audit.py``).
    """
    if not daemon_url:
        return DEFAULT_AUDIT_ENDPOINT
    return f"{daemon_url.rstrip('/')}{AUDIT_EMIT_PATH}"


                                                                               
 
                                                                           
                                                 
                                                                      
                                            
                                                                         
                                                                           
                                                             
 
                                                                         
                                                                          
# or during chaos rotation). It MUST stay in lockstep with the TOML
                                                                      
                                                                          
                           
 
                                                                     
                                                                       
                                                                         
                                 
 
                                                                           
                                                                      
_ALL_PLATFORMS_ENABLED: frozenset[Platform] = frozenset(
    {
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
        Platform.MARKDOWN_FALLBACK,
    }
)
_DOCTRINE_ENABLED: dict[str, frozenset[Platform]] = {
                                                                     
                                                                 
                                                                 
    "max-scope": _ALL_PLATFORMS_ENABLED,
                                                                   
                                                                 
                                                                 
    "default": _ALL_PLATFORMS_ENABLED,
                                                                         
                                                                 
                                 
    "capa-firewall": frozenset(
        {
            Platform.INK,                
            Platform.EMAIL,                                
            Platform.MARKDOWN_FALLBACK,
                                                                
        }
    ),
}


class Renderer(ABC):
    """Base Renderer ABC.

    Concrete subclasses MUST set ``PLATFORM`` class attribute and implement
    ``render(result: AugmentationResult) -> RenderResult``. ``audit_anchor``
    is provided as a default implementation that POSTs ``CitationRendered``
    events to the canonical daemon ``/v1/audit/emit`` endpoint via httpx
    (wire shape mirrors ``internal/daemon/handlers/audit_emit.go``
    ``AuditEventIn``). Subclasses may override for custom audit semantics
    (e.g., voice may attach additional payload fields like ``duration_ms``).

    Constructor accepts an optional ``daemon_url`` keyword that the
    ``register_default_renderers`` factory threads in from the plugin's
    ``register(ctx)`` wiring; the renderer stores the derived audit
    endpoint as ``self._audit_endpoint``. ``audit_anchor`` defaults to
    that instance attribute when no per-call ``audit_endpoint`` override
    is supplied.
    """

    PLATFORM: Platform                     

    def __init__(self, *, daemon_url: str | None = None) -> None:
        """Wire the optional ``daemon_url`` into a per-instance audit endpoint.

        ``daemon_url=None`` → ``DEFAULT_AUDIT_ENDPOINT`` (canonical
        ``http://localhost:4471/v1/audit/emit``). A non-None value is
        normalized + composed with ``/v1/audit/emit``.

        Concrete subclasses that already define ``__init__`` (e.g.,
        ``TelegramCitationRenderer(active_project=...)``) MUST forward
        ``daemon_url`` via ``super().__init__(daemon_url=daemon_url)``
        so the operator-configured URL still propagates.
        """
        self._audit_endpoint: str = _derive_audit_endpoint(daemon_url)

    def __init_subclass__(cls, **kwargs: Any) -> None:
        """Enforce PLATFORM attribute presence on every concrete subclass.

        Fires at class-creation time (subclass body evaluated), so an
        author forgetting to set ``PLATFORM`` sees the failure immediately
        at import — not later at instantiation.

        Only checked when ``render`` is concrete; partial subclasses (no
        ``render`` body) are still abstract and may defer PLATFORM
        assignment to a further subclass.
        """
        super().__init_subclass__(**kwargs)
        render_method = cls.__dict__.get("render")
        if render_method is None or getattr(render_method, "__isabstractmethod__", False):
            return
                                                                        
                                                  
        own_platform = cls.__dict__.get("PLATFORM")
        inherited = any(
            getattr(base, "PLATFORM", None) is not None
            for base in cls.__mro__[1:]
            if base is not Renderer and isinstance(base, type)
        )
        if own_platform is None and not inherited:
            raise TypeError(
                f"Renderer subclass {cls.__name__} must set PLATFORM class attribute"
            )

    @abstractmethod
    def render(self, result: AugmentationResult) -> RenderResult:
        """Render an `AugmentationResult` to platform-native output.

        MUST NOT raise on valid envelope; the registry's dispatch wraps
        the call in an exception-safe fallback. Subclasses should
        return an empty/placeholder output for ``result.citations == []``
        rather than raising.
        """

    @staticmethod
    def _build_wrapper_meta(
        result: AugmentationResult,
        *,
        include_cache_key: bool = False,
    ) -> dict[str, Any]:
        """Construct the shared per-renderer metadata dict (M-1 fix-cycle).

        Default fields (every renderer): ``request_id``, ``session_id``,
        ``doctrine``, ``project_id``, ``audit_event_id``,
        ``kg_token_count``, ``emitted_at`` (RFC 3339 string), and
        ``citation_count``. Renderers that previously emitted a subset
        now emit the full set — strictly additive, so downstream
        consumers (TUI panels, AFK card, Phase D AFK richness builder)
        gain context, never lose it.

        ``include_cache_key=True`` adds ``cache_key_hash``. Ink uses this
        today because the TUI surfaces cache hit/miss state; other
        renderers don't need it. Future renderers can opt in.

        ``emitted_at`` is normalized to ``isoformat()`` string (the
        downstream JSON pipeline doesn't keep ``datetime`` types).
        """
        meta: dict[str, Any] = {
            "request_id": result.request_id,
            "session_id": result.session_id,
            "doctrine": result.doctrine,
            "project_id": result.project_id,
            "audit_event_id": result.audit_event_id,
            "kg_token_count": result.kg_token_count,
            "emitted_at": result.emitted_at.isoformat(),
            "citation_count": len(result.citations),
        }
        if include_cache_key:
            meta["cache_key_hash"] = result.cache_key_hash
        return meta

    def audit_anchor(
        self,
        citation: Envelope,
        *,
        doctrine: str,
        rendered_at: datetime | None = None,
        audit_endpoint: str | None = None,
    ) -> AuditEventID:
        """Emit ``CitationRendered`` Tessera event; returns audit event ID.

        POSTs to the canonical daemon ``/v1/audit/emit`` endpoint
        (``internal/daemon/server.go:737`` +
        ``internal/daemon/handlers/audit_emit.go``). The wire shape
        mirrors the Go-side ``citationadapter.Adapter.EmitCitationRendered``
        byte-for-byte so daemon-side
        ``server_audit_query.go::extractDoctrineFromPayload`` keeps
        returning the real doctrine instead of fail-closing to
        ``capa-firewall``.

        Wire shape (``AuditEventIn``)::

            POST <audit_endpoint>
            {
              "project_id": "<citation.project_id>",
              "type":       "CitationRendered",
              "payload": {
                "citation_id":      "<citation.id>",
                "platform":         "<self.PLATFORM.value>",
                "audit_event_link": "zen://audit/<event-id>",
                "rendered_at":      "<RFC 3339 Z>",
                "doctrine":         "<session doctrine>"
              }
            }

            202 Accepted
            {"id": "<uuidv4>", "accepted": true, "emitted_at": NNN}

        Parameters
        ----------
        citation:
            The rendered ``Envelope``. ``project_id`` comes from the
            envelope (load-bearing — the audit row is project-scoped per
            spec §3.1) and ``audit_event_link`` from
            ``citation.audit_event_url()`` (the ``zen://audit/<id>``
            deep-link form, NOT the raw event id).
        doctrine:
            The session doctrine (one of ``max-scope``, ``default``,
            ``capa-firewall``). Concrete renderers pass
            ``result.doctrine`` through from the wrapping
            ``AugmentationResult`` so the audit row records the
            doctrine in force at render time.
        rendered_at:
            Optional explicit render timestamp (UTC, tz-aware). ``None``
            → ``datetime.now(UTC)`` (mirrors the Go adapter's behaviour
            when the renderer doesn't stamp a time). Serialised RFC 3339
            with trailing ``Z`` (matches Go ``time.Now().UTC().Format(
            time.RFC3339)``).
        audit_endpoint:
            Optional per-call URL override. ``None`` → the instance
            attribute set by ``__init__`` (derived from ``daemon_url``).

        Failure mode: log a warning and return an empty string. Audit
        anchoring is a side channel; rendering is non-fatal under audit
        failure to preserve operator-visible output (inv-zen-166).

        Subclasses may override to add platform-specific payload fields
        (e.g., voice may attach ``duration_ms``); the canonical contract
        keys must remain present so the daemon's hash-chain stays
        well-formed (inv-zen-051).
        """
        endpoint = audit_endpoint if audit_endpoint is not None else self._audit_endpoint
                                                                         
                                                                
        import httpx  # noqa: PLC0415

                                                                          
                                                                         
                                                     
                                                                         
        rendered_at_utc = (rendered_at or datetime.now(timezone.utc)).astimezone(
            timezone.utc
        )
        rendered_at_iso = rendered_at_utc.strftime("%Y-%m-%dT%H:%M:%SZ")

        body: dict[str, Any] = {
            "project_id": citation.project_id,
            "type": "CitationRendered",
            "payload": {
                "citation_id": citation.id,
                "platform": self.PLATFORM.value,
                "audit_event_link": citation.audit_event_url(),
                "rendered_at": rendered_at_iso,
                "doctrine": doctrine,
            },
        }
        try:
            with httpx.Client(timeout=2.0) as client:
                resp = client.post(endpoint, json=body)
                resp.raise_for_status()
                data = resp.json()
                if not isinstance(data, dict):
                    return ""
                                                                        
                                                              
                                                            
                return str(data.get("id", ""))
        except Exception as exc:  # noqa: BLE001 — intentional broad catch
            _log.warning(
                "audit_anchor failed for citation %s on %s: %s",
                citation.id,
                self.PLATFORM.value,
                exc,
            )
            return ""


class RendererRegistry:
    """Dispatches ``AugmentationResult`` to registered concrete renderers;
    falls back to markdown on failure.

    The Hermes plugin loader invokes ``dispatch()`` per AugmentationResult
    reaching the rendering pipeline (via zen-side hook callbacks). Doctrine
    filter applied first; then renderer lookup; then exception-safe wrapper.
    """

    def __init__(self) -> None:
        self._renderers: dict[Platform, Renderer] = {}

    def register(self, renderer: Renderer) -> None:
        """Register concrete renderer; replaces existing (last-wins) per platform."""
        self._renderers[renderer.PLATFORM] = renderer

    def is_enabled(self, platform: Platform, doctrine: str) -> bool:
        """Doctrine-aware enable/disable check. Privacy boundary integration.

        Unknown doctrine → returns False (defensive default).
        """
        enabled = _DOCTRINE_ENABLED.get(doctrine, frozenset())
        return platform in enabled

    def dispatch(self, result: AugmentationResult, platform: Platform) -> RenderResult:
        """Render `result` for `platform`; falls back to markdown on failure or doctrine disable."""
                                               
        if not self.is_enabled(platform, result.doctrine):
            _log.info(
                "renderer %s disabled by doctrine %s; emitting markdown fallback",
                platform.value,
                result.doctrine,
            )
            return self._emit_markdown_fallback(result)

                            
        renderer = self._renderers.get(platform)
        if renderer is None:
            _log.warning(
                "no renderer registered for platform %s; emitting markdown fallback",
                platform.value,
            )
            return self._emit_markdown_fallback(result)

                                                  
        try:
            return renderer.render(result)
        except Exception as exc:  # noqa: BLE001 — intentional broad catch for fallback safety
            _log.exception(
                "renderer %s failed: %s; emitting markdown fallback",
                platform.value,
                exc,
            )
            return self._emit_markdown_fallback(result)

    def _emit_markdown_fallback(self, result: AugmentationResult) -> RenderResult:
        """Emit markdown fallback rendering (byte-exact parity with Plan 11
        substrate ``internal/citation/markdown_fallback.go::renderFootnote``).

        This is a TRUE fallback — the operator never sees an unhandled
        error. Output matches what the Plan 11 Go substrate would emit if
        no plugin renderers were registered at all (universal degradation).

        Per-citation footnote (matches Go renderFootnote byte-for-byte):

            ``[^<citation_id>]\\n\\n[^<citation_id>]: <payload> ``
            ``([zen://audit/<event-id>](zen://audit/<event-id>); ``
            ``project=<p>; doctrine=<d>; lane=<l>; conf=<conf:.2f>``
            ``[; expires=<RFC3339>])``

        Multi-citation wrappers concatenate per-citation footnotes
        separated by ``\\n\\n``. Empty wrapper emits ``*(no citations)*``
        (a wrapper-level placeholder; Go substrate errors on a nil
        envelope and never reaches an empty path).

        I-2 fix-cycle: pre-fix, the Python output used numeric footnote
        labels (``[^1]``, ``[^2]``) and a multi-line indented metadata
        block; the Go substrate has always emitted citation-ID labels
        plus a parenthesised single-line metadata suffix. Cross-language
        parity is verified by
        ``tests/renderers/test_markdown_fallback_go_parity.py`` (golden
        oracle: ``bin/zen-markdown-fallback-golden``).
        """
        if not result.citations:
            return RenderResult(
                platform=Platform.MARKDOWN_FALLBACK,
                output="*(no citations)*",
                metadata={
                    "request_id": result.request_id,
                    "session_id": result.session_id,
                    "doctrine": result.doctrine,
                    "kg_token_count": result.kg_token_count,
                    "citation_count": 0,
                },
                audit_event_ids=[],
            )

        footnotes: list[str] = [
            self._render_footnote(c, doctrine=result.doctrine) for c in result.citations
        ]
        return RenderResult(
            platform=Platform.MARKDOWN_FALLBACK,
            output="\n\n".join(footnotes),
            metadata={
                "request_id": result.request_id,
                "session_id": result.session_id,
                "doctrine": result.doctrine,
                "kg_token_count": result.kg_token_count,
                "citation_count": len(result.citations),
            },
            audit_event_ids=[c.audit_event_id for c in result.citations],
        )

    @staticmethod
    def _render_footnote(envelope: Envelope, *, doctrine: str) -> str:
        """Build the byte-exact CommonMark footnote for a single envelope.

        Mirrors ``internal/citation/markdown_fallback.go::renderFootnote``
        line-for-line. The Go format is the source-of-truth; this
        function exists to keep the Python wrapper-level fallback in
        cross-language parity. See
        ``tests/renderers/test_markdown_fallback_go_parity.py`` for the
        oracle-verified parity contract.
        """
        audit_url = envelope.audit_event_url()
        parts: list[str] = [
            f"[^{envelope.id}]\n\n",
            f"[^{envelope.id}]: ",
            envelope.payload,
            " ([",
            audit_url,
            "](",
            audit_url,
            "); project=",
            envelope.project_id,
            "; doctrine=",
            doctrine,
            "; lane=",
            envelope.retrieval_lane.value,
            "; conf=",
            f"{envelope.confidence:.2f}",
        ]
        if envelope.expiration is not None:
                                                                    
                                                      
            exp_utc = envelope.expiration.astimezone(timezone.utc)
            parts.append("; expires=")
            parts.append(exp_utc.strftime("%Y-%m-%dT%H:%M:%SZ"))
        parts.append(")")
        return "".join(parts)


def register_default_renderers(
    registry: RendererRegistry,
    *,
    daemon_url: str | None = None,
) -> None:
    """Register the 6 platform-specific renderers shipped en Plan 12 Phase A.

    Called from the plugin's ``__init__.py register(ctx)`` function on
    plugin load. Imports are deferred to avoid circular dependencies
    (each concrete renderer module imports ``Renderer`` base class from
    this module).

    ``daemon_url`` (optional): non-default daemon TCP URL to thread
    through every concrete renderer's ``__init__``. ``None`` → each
    renderer keeps the canonical ``DEFAULT_DAEMON_URL`` default
    (``http://localhost:4471``) per the cmd/zen-mcp-{budget,audit} doc
    contract. The plugin's ``register(ctx)`` typically reads the
    operator-configured URL from ``ctx.config`` (Hermes plugin context
    surface) and forwards it here so audit-anchor side-channel calls
    reach the correct host.
    """
    from hermes_plugins.hades.renderers.email_citation import EmailCitationRenderer
    from hermes_plugins.hades.renderers.ink_citation import InkCitationRenderer
    from hermes_plugins.hades.renderers.slack_citation import SlackCitationRenderer
    from hermes_plugins.hades.renderers.telegram_citation import (
        TelegramCitationRenderer,
    )
    from hermes_plugins.hades.renderers.voice_citation import VoiceCitationRenderer
    from hermes_plugins.hades.renderers.web_citation import WebCitationRenderer

    registry.register(InkCitationRenderer(daemon_url=daemon_url))
    registry.register(TelegramCitationRenderer(daemon_url=daemon_url))
    registry.register(SlackCitationRenderer(daemon_url=daemon_url))
    registry.register(EmailCitationRenderer(daemon_url=daemon_url))
    registry.register(VoiceCitationRenderer(daemon_url=daemon_url))
    registry.register(WebCitationRenderer(daemon_url=daemon_url))
