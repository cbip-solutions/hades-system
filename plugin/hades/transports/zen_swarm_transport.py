# SPDX-License-Identifier: MIT
"""ZenSwarmTransport — Python class that forwards LLM requests via HTTP"""

from __future__ import annotations

import json
import os
from collections.abc import Iterator
from dataclasses import dataclass, field
from typing import Any

import httpx


@dataclass(frozen=True)
class CompletedResponse:
    """Result of a ``ZenSwarmTransport.complete`` (or single-chunk
    ``stream``) call.

    Fields:
        status: HTTP status of the upstream provider response, as observed
            by the daemon and forwarded back through the
            ``ForwardedResponse`` envelope. 200 is the success case;
            non-200 status codes can occur when the daemon's dispatcher
            received an upstream-provider response with a 4xx/5xx body
            (e.g. provider rate-limit 429). The transport raises
            ``RuntimeError`` for daemon-side failures (502 / malformed
            envelope) BEFORE returning a ``CompletedResponse``, so a
            non-200 status here means the upstream provider returned that
            status verbatim.
        body: Parsed JSON body the upstream provider returned (the
            Anthropic ``Message`` shape for the canonical path, or
            ``{"type": "error", ...}`` for provider errors). Equivalent
            to the previous ``complete()`` return type — wrapping it in
            this dataclass adds metadata, doesn't change content.
        headers: Upstream-response headers, with secret-shaped header
            names already stripped by the daemon (see
            ``filteredResponseHeaders`` in
            ``internal/daemon/transport/messages_handler.go``). Common
            useful keys: ``anthropic-ratelimit-requests-remaining``,
            ``request-id``, ``anthropic-organization-id``.
        audit_event_id: the release design Tessera-anchored audit event ID the
            daemon assigned to this dispatch. the release design citation renderers
            deep-link operator-facing UIs via ``zen://audit/<id>`` URIs;
            losing this metadata at the transport boundary breaks the
            audit-anchor surface. Empty string when the daemon's anchor
            is offline (graceful degradation per
            ``messages_handler.go:emitAnchor``).

    Frozen dataclass: post-construction mutation is forbidden so callers
    cannot accidentally corrupt the audit-trail or upstream metadata
    while passing the response around.
    """

    status: int
    body: dict[str, Any]
    headers: dict[str, str] = field(default_factory=dict)
    audit_event_id: str = ""

HEADER_TRANSPORT_SOURCE = "X-Zen-Transport"
TRANSPORT_LABEL = "zenswarm"

# Headers that MUST NEVER cross the Python ↔ Go boundary. Anything that
# smells like an authorisation header is dropped at the transport boundary.
# Defence in depth: even if the caller had a bug that included a secret,
# the Go-side handler ALSO strips these (cf. internal/daemon/transport/
# messages_handler.go isSecretHeader), but we drop client-side first.
_FORBIDDEN_HEADERS = frozenset(
    {
        "authorization",
        "anthropic-auth-token",
        "x-api-key",
        "cookie",
        "set-cookie",
    }
)

# Kwargs that ZenSwarmTransport.complete handles itself (envelope metadata);
# they are NOT forwarded into the Anthropic-format inner body.
_ENVELOPE_KWARGS = frozenset(
    {
        "session_id",
        "conversation_id",
        "idempotency_key",
        "profile",
        "project",
        "extra_headers",
    }
)


class ZenSwarmTransport:
    """LLM-dispatch transport that routes via zen-swarm-ctld.

    Construct once per caller and reuse — the underlying ``httpx.Client``
    pools connections to the daemon Unix socket. Call ``close()`` at
    shutdown to release the client.

    NOTE: this class is the substrate for DIRECT callers (zen CLI Python
    wrappers, MCPs, integration tests). Hermes itself does NOT drive this
    class — Hermes uses the ``ProviderProfile`` registered in
    ``plugin/zen-swarm/providers/__init__.py`` to point its native
    ``anthropic.Anthropic`` SDK at the daemon directly. No
    ``ProviderTransport`` alias is exported — the real Hermes
    ``ProviderTransport`` ABC (``agent/transports/base.py:16``) is a
    format adapter for a different purpose; conflating the two would
    mislead reviewers.
    """

    def __init__(self, socket_path: str | None = None) -> None:
        """Construct the transport.

        Args:
            socket_path: Optional explicit override for the daemon Unix-
                socket path. Falls back to ``$ZEN_DAEMON_SOCKET`` or
                ``/tmp/zen-swarm.sock``. Explicit argument wins over env.
        """
        self._socket_path = socket_path or os.environ.get(
            "ZEN_DAEMON_SOCKET", "/tmp/zen-swarm.sock"
        )
        self._client = self._build_default_client()
        self._closed = False

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def complete(
        self,
        messages: list[dict[str, Any]],
        model: str,
        *,
        session_id: str | None = None,
        conversation_id: str | None = None,
        idempotency_key: str | None = None,
        profile: str | None = None,
        project: str | None = None,
        max_tokens: int | None = None,
        extra_headers: dict[str, str] | None = None,
        **kwargs: Any,
    ) -> CompletedResponse:
        """Forward a completion request to the daemon.

        Returns a :class:`CompletedResponse` exposing the upstream
        provider's parsed JSON body, the upstream HTTP status, the
        upstream response headers (with secret-shaped keys stripped by
        the daemon), and the the release design Tessera ``audit_event_id`` the daemon
        assigned. release track reviewer I2: the prior return type was just
        the inner body dict, which silently discarded ``status``,
        ``headers``, and ``audit_event_id`` — the release design citation renderers
        consume ``audit_event_id`` for ``zen://audit/<id>`` deep links.

        Raises:
            RuntimeError: when the daemon returns a non-2xx status (a
                transport-layer failure as opposed to a 4xx/5xx upstream
                provider response) or any envelope/body parse fails.
                Callers that need to distinguish upstream-vs-transport
                errors should wrap with their own error type.
        """
        envelope = self._build_envelope(
            messages=messages,
            model=model,
            session_id=session_id,
            conversation_id=conversation_id,
            idempotency_key=idempotency_key,
            profile=profile,
            project=project,
            max_tokens=max_tokens,
            kwargs=kwargs,
        )
        headers = self._build_request_headers(extra_headers)
        response = self._client.post(
            "/v1/messages",
            content=json.dumps(envelope).encode("utf-8"),
            headers=headers,
        )
        if response.status_code != 200:
            raise RuntimeError(
                f"zen-swarm daemon returned status {response.status_code}: "
                f"{response.text!r}"
            )
        try:
            forwarded = response.json()
        except json.JSONDecodeError as exc:
            raise RuntimeError(
                f"daemon returned malformed ForwardedResponse envelope: {exc}"
            ) from exc
        body_text = forwarded.get("body", "")
        if isinstance(body_text, bytes):
            body_text = body_text.decode("utf-8", errors="replace")
        try:
            parsed_body = json.loads(body_text)
        except json.JSONDecodeError as exc:
            raise RuntimeError(
                f"daemon returned non-JSON upstream body: {exc}"
            ) from exc
        # Normalise upstream metadata. ``status`` defaults to 200 because
        # the daemon emits status=200 envelope-wraps every dispatcher
        # success; missing ``headers`` becomes an empty dict to keep
        # caller code path uniform; missing ``audit_event_id`` becomes
        # an empty string (graceful degradation when the release design anchor offline).
        upstream_status = forwarded.get("status")
        if not isinstance(upstream_status, int):
            upstream_status = 200
        upstream_headers = forwarded.get("headers") or {}
        if not isinstance(upstream_headers, dict):
            upstream_headers = {}
        audit_event_id = forwarded.get("audit_event_id") or ""
        if not isinstance(audit_event_id, str):
            audit_event_id = ""
        return CompletedResponse(
            status=upstream_status,
            body=parsed_body,
            headers=upstream_headers,
            audit_event_id=audit_event_id,
        )

    def stream(
        self,
        messages: list[dict[str, Any]],
        model: str,
        **kwargs: Any,
    ) -> Iterator[CompletedResponse]:
        """Stream a completion.

        release track yields a single :class:`CompletedResponse` wrapping the
        full response (semantically equivalent to non-streaming for
        callers); release track extends to true incremental SSE relay where
        each chunk yields a partial-body CompletedResponse.

        Consumers that expect a generator handle single-chunk yields
        gracefully — the on-screen rendering happens after the first chunk
        arrives, which is what ``complete`` provides anyway. SSE incremental
        relay is a UX optimization that lands when the augmentation pipeline
        (release track) ships SSE-aware response forwarding.
        """
        yield self.complete(messages=messages, model=model, **kwargs)

    def close(self) -> None:
        """Release the httpx client. Idempotent — calling twice is safe."""
        if self._closed:
            return
        try:
            self._client.close()
        finally:
            self._closed = True

    # ------------------------------------------------------------------
    # Test seam
    # ------------------------------------------------------------------

    def _set_client_for_test(self, client: httpx.Client) -> None:
        """Test-only seam: replace the underlying httpx.Client.

        Production wiring uses Unix-socket transport; tests inject an
        ``httpx.Client`` backed by an ``httpx.MockTransport`` or by a
        ``httptest.Server`` URL for end-to-end integration. The previous
        client (if any) is closed before replacement.
        """
        if self._client is not None:
            try:
                self._client.close()
            except Exception:  # pragma: no cover - defensive cleanup
                pass
        self._client = client
        self._closed = False

    # ------------------------------------------------------------------
    # Internals
    # ------------------------------------------------------------------

    def _build_default_client(self) -> httpx.Client:
        """Build the production Unix-socket httpx.Client."""
        unix_transport = httpx.HTTPTransport(uds=self._socket_path)
        return httpx.Client(
            transport=unix_transport,
            base_url="http://localhost",
            timeout=httpx.Timeout(connect=5.0, read=120.0, write=30.0, pool=5.0),
        )

    def _build_envelope(
        self,
        *,
        messages: list[dict[str, Any]],
        model: str,
        session_id: str | None,
        conversation_id: str | None,
        idempotency_key: str | None,
        profile: str | None,
        project: str | None,
        max_tokens: int | None,
        kwargs: dict[str, Any],
    ) -> dict[str, Any]:
        """Build the ForwardedRequest JSON envelope.

        Wire format mirror of internal/daemon/transport/types.go ForwardedRequest.
        Non-envelope kwargs flow into the inner Anthropic body so providers
        see them (system prompt, temperature, etc.).
        """
        inner_body: dict[str, Any] = {
            "model": model,
            "messages": messages,
        }
        if max_tokens is not None:
            inner_body["max_tokens"] = max_tokens
        for key, value in kwargs.items():
            if key in _ENVELOPE_KWARGS:
                continue
            inner_body[key] = value

        envelope: dict[str, Any] = {
            "body": json.dumps(inner_body),
            "transport_source": TRANSPORT_LABEL,
            "model": model,
        }
        if session_id:
            envelope["session_id"] = session_id
        if conversation_id:
            envelope["conversation_id"] = conversation_id
        if idempotency_key:
            envelope["idempotency_key"] = idempotency_key
        if profile:
            envelope["profile"] = profile
        if project:
            envelope["project"] = project
        return envelope

    def _build_request_headers(
        self, extra_headers: dict[str, str] | None
    ) -> dict[str, str]:
        """Build the HTTP request headers, dropping forbidden ones."""
        headers: dict[str, str] = {
            "Content-Type": "application/json",
            HEADER_TRANSPORT_SOURCE: TRANSPORT_LABEL,
        }
        for name, value in self._sanitise_headers(extra_headers).items():
            if name not in headers:
                headers[name] = value
        return headers

    @staticmethod
    def _sanitise_headers(
        extra_headers: dict[str, str] | None,
    ) -> dict[str, str]:
        """Drop forbidden header names case-insensitively."""
        if not extra_headers:
            return {}
        sanitised: dict[str, str] = {}
        for name, value in extra_headers.items():
            if name.lower() in _FORBIDDEN_HEADERS:
                continue
            sanitised[name] = value
        return sanitised
