# SPDX-License-Identifier: MIT
                                          
"""HTML email renderer for citation envelopes (Plan 12 Phase A Task A-6)."""

from __future__ import annotations

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)
from markupsafe import Markup, escape

_PER_CITATION_PAYLOAD_CAP = 600


class EmailCitationRenderer(Renderer):
    """Renders an `AugmentationResult` to an HTML email body string."""

    PLATFORM = Platform.EMAIL

    def __init__(
        self,
        web_fallback_audit_url: bool = False,
        audit_web_base_url: str = "https://hades.local",
        *,
        daemon_url: str | None = None,
    ) -> None:
        """``web_fallback_audit_url``: substitute ``zen://audit/<id>`` with https form.

        ``audit_web_base_url``: base URL for the fallback (default
        ``https://hades.local`` per plan 18b phase D brand pass).
        Operator configures via hades-side config
        (``~/.hermes/config.yaml`` operator section or daemon env
        var) when daemon exposes a public audit web UI (Plan 13+).

        ``daemon_url`` is forwarded to the base ``Renderer`` so the
        instance-level audit endpoint (``self._audit_endpoint``) honors
        an operator-configured daemon TCP URL when supplied (Fix-cycle
        I-1).
        """
        super().__init__(daemon_url=daemon_url)
        self._web_fallback = web_fallback_audit_url
        self._audit_base = audit_web_base_url.rstrip("/")

    def render(self, result: AugmentationResult) -> RenderResult:
        body_rows: list[str] = []
        audit_ids: list[str] = []

        if not result.citations:
            body_rows.append(
                '<tr><td style="padding:16px;color:#666666;font-style:italic;">'
                "(no citations)</td></tr>"
            )
        else:
            for idx, citation in enumerate(result.citations, start=1):
                body_rows.append(self._build_citation_block(citation, idx))
                audit_ids.append(
                    self.audit_anchor(
                        citation,
                        doctrine=result.doctrine,
                        rendered_at=result.emitted_at,
                    )
                )

        footer = self._build_footer(result)
        html = self._wrap_document(
            inner_rows="\n".join(body_rows),
            footer=footer,
            citation_count=len(result.citations),
        )

        return RenderResult(
            platform=Platform.EMAIL,
            output=html,
            metadata=self._build_wrapper_meta(result),
            audit_event_ids=audit_ids,
        )

    def _build_citation_block(self, citation: Envelope, index: int) -> str:
        """Build one ``<tr>`` row containing a styled inner ``<table>`` for the citation."""
        payload_e = escape(citation.payload[:_PER_CITATION_PAYLOAD_CAP])
        source_e = escape(citation.source.value)
        lane_e = escape(citation.retrieval_lane.value)
        project_e = escape(citation.project_id)
        confidence_str = f"{citation.confidence:.2f}"
        audit_url = self._audit_url_for(citation)
        audit_url_e = escape(audit_url)

                                                                      
                                                                           
                                                                          
                                                         
        block = Markup("""
<tr>
  <td style="padding:12px 16px;background-color:#ffffff;border:1px solid #e1e4e8;border-radius:4px;">
    <table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%">
      <tr>
        <td style="font-size:13px;color:#586069;padding-bottom:4px;">[{idx}]</td>
      </tr>
      <tr>
        <td style="font-weight:600;font-size:14px;color:#24292e;padding-bottom:8px;line-height:1.5;">{payload}</td>
      </tr>
      <tr>
        <td style="font-size:12px;color:#586069;padding-bottom:6px;">
          Source: <code style="background:#f6f8fa;padding:2px 4px;border-radius:3px;">{source}</code>
          | Lane: <code style="background:#f6f8fa;padding:2px 4px;border-radius:3px;">{lane}</code>
          | Project: <code style="background:#f6f8fa;padding:2px 4px;border-radius:3px;">{project}</code>
          | Confidence: <code style="background:#f6f8fa;padding:2px 4px;border-radius:3px;">{confidence}</code>
        </td>
      </tr>
      <tr>
        <td style="font-size:12px;padding-top:6px;">
          <a href="{audit_url}" style="color:#0366d6;text-decoration:none;">Audit chain &rarr; {audit_url_text}</a>
        </td>
      </tr>
    </table>
  </td>
</tr>
""").format(
            idx=index,
            payload=payload_e,
            source=source_e,
            lane=lane_e,
            project=project_e,
            confidence=confidence_str,
            audit_url=audit_url_e,
            audit_url_text=audit_url_e,
        )
        return str(block)

    def _audit_url_for(self, citation: Envelope) -> str:
        """Return the canonical or HTTPS-fallback audit URL for a citation.

        Default (``web_fallback_audit_url=False``) returns the canonical
        zen:// deep-link via ``Envelope.audit_event_url()`` (the
        cross-language helper that mirrors Go's ``AuditEventURL``).
        Cross-renderer consistency check (M-2 fix-cycle): web + ink also
        use this helper.

        When ``web_fallback_audit_url=True`` is passed at construction,
        substitutes an HTTPS form (``<audit_web_base_url>/audit/<id>``)
        for older email clients that strip non-``http(s)`` /``mailto:``
        schemes. The HTTPS substitute exists because email is the only
        renderer whose downstream surface (an MUA / webmail client) may
        not register the ``zen://`` URL handler.

        Plan 13+ may add a daemon-side HTTPS audit redirector that
        consumes the same path; the email HTTPS form is a forward-
        compatible alias for that surface.
        """
        if self._web_fallback:
            return f"{self._audit_base}/audit/{citation.audit_event_id}"
        return citation.audit_event_url()

    @staticmethod
    def _build_footer(result: AugmentationResult) -> str:
        return str(
            Markup("""
<tr>
  <td style="padding:16px 0;font-size:11px;color:#959da5;border-top:1px solid #e1e4e8;">
    Doctrine: <code style="background:#f6f8fa;padding:1px 3px;border-radius:2px;">{doctrine}</code>
    | KG tokens: <code style="background:#f6f8fa;padding:1px 3px;border-radius:2px;">{tokens}</code>
    | Cache: <code style="background:#f6f8fa;padding:1px 3px;border-radius:2px;">{cache}</code>
    | Request: <code style="background:#f6f8fa;padding:1px 3px;border-radius:2px;">{request}</code>
  </td>
</tr>
""").format(
                doctrine=escape(result.doctrine),
                tokens=result.kg_token_count,
                cache=escape(result.cache_key_hash),
                request=escape(result.request_id),
            )
        )

    @staticmethod
    def _wrap_document(*, inner_rows: str, footer: str, citation_count: int) -> str:
        """Top-level HTML5 document with inline CSS + max-width:600px container."""
        return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
  <title>HADES citations ({citation_count})</title>
</head>
<body style="margin:0;padding:0;background-color:#f6f8fa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="background-color:#f6f8fa;">
    <tr>
      <td align="center" style="padding:20px 0;">
        <table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="max-width:600px;background-color:#ffffff;border-radius:6px;padding:20px;">
          <tr>
            <td>
              <h2 style="margin:0 0 12px 0;font-size:20px;color:#24292e;">Citations ({citation_count})</h2>
              <table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%">
                {inner_rows}
              </table>
              {footer}
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>"""
