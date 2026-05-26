# SPDX-License-Identifier: MIT
"""Cross-language parity test: Python markdown fallback ↔ Go substrate."""

from __future__ import annotations

import os
import subprocess
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import (
    Renderer,
    RendererRegistry,
    register_default_renderers,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RetrievalLane,
)

                                                                             
                                                                           
                                                                             


@dataclass(frozen=True)
class _ParityCase:
    """A frozen parity case: a single envelope + the byte-exact Go output."""

    name: str
    envelope: Envelope
    doctrine: str                                  
    expected_footnote: str                                       


def _make_case(
    *,
    name: str,
    citation_id: str,
    payload: str,
    audit_event_id: str,
    project_id: str,
    doctrine: str,
    lane: RetrievalLane,
    confidence: float,
    expiration: datetime | None = None,
    source: CitationSource = CitationSource.CARONTE_QUERY,
    type_: CitationType = CitationType.KG_NODE,
) -> _ParityCase:
    env = Envelope(
        id=citation_id,
        type=type_,
        source=source,
        retrieval_lane=lane,
        audit_event_id=audit_event_id,
        confidence=confidence,
        rrf_score=0.01,
        rrf_rank=0,
        project_id=project_id,
        payload=payload,
        expiration=expiration,
    )
    audit_url = f"zen://audit/{audit_event_id}"
    expected = (
        f"[^{citation_id}]\n\n"
        f"[^{citation_id}]: {payload}"
        f" ([{audit_url}]({audit_url}); "
        f"project={project_id}; "
        f"doctrine={doctrine}; "
        f"lane={lane.value}; "
        f"conf={confidence:.2f}"
    )
    if expiration is not None:
                                                                            
                                      
        expiration_str = expiration.astimezone(timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        expected += f"; expires={expiration_str}"
    expected += ")"
    return _ParityCase(
        name=name, envelope=env, doctrine=doctrine, expected_footnote=expected
    )


_PARITY_CASES: list[_ParityCase] = [
    _make_case(
        name="basic-caronte-semantic-default",
        citation_id="c-test0001",
        payload="MergeEngine.Score()",
        audit_event_id="evt-0001",
        project_id="p",
        doctrine="default",
        lane=RetrievalLane.SEMANTIC,
        confidence=0.5,
    ),
    _make_case(
        name="max-scope-with-high-confidence",
        citation_id="c-cabba9efa",
        payload="WorkforceQueue priority enum amendment 0042 added URGENT slot",
        audit_event_id="evt-cabba9efa",
        project_id="zen-swarm",
        doctrine="max-scope",
        lane=RetrievalLane.LEXICAL,
        confidence=0.94,
    ),
    _make_case(
        name="capa-firewall-with-expiration",
        citation_id="c-de4dbeef",
        payload="ProjectArchiveSweep tombstones",
        audit_event_id="evt-de4dbeef",
        project_id="alpha",
        doctrine="capa-firewall",
        lane=RetrievalLane.GRAPH,
        confidence=0.71,
        expiration=datetime(2026, 5, 11, 12, 0, 0, tzinfo=timezone.utc),
    ),
    _make_case(
        name="zero-confidence-boundary",
        citation_id="c-zero",
        payload="payload",
        audit_event_id="evt-zero",
        project_id="p",
        doctrine="default",
        lane=RetrievalLane.RERANK,
        confidence=0.0,
    ),
    _make_case(
        name="one-confidence-boundary",
        citation_id="c-ones",
        payload="payload",
        audit_event_id="evt-ones",
        project_id="p",
        doctrine="default",
        lane=RetrievalLane.TEMPORAL,
        confidence=1.0,
    ),
]


                                                                             
                                                            
                                                                             


_REPO_ROOT = Path(__file__).resolve().parents[4]
_GO_GOLDEN_BIN = _REPO_ROOT / "bin" / "zen-markdown-fallback-golden"


def _go_golden_oracle_available() -> bool:
    """The Go golden-generator binary is present + executable."""
    return _GO_GOLDEN_BIN.exists() and os.access(_GO_GOLDEN_BIN, os.X_OK)


def _invoke_go_oracle(case: _ParityCase) -> str:
    """Invoke the Go substrate via the parity-oracle binary; return its output."""
    proc = subprocess.run(
        [
            str(_GO_GOLDEN_BIN),
            "-id",
            case.envelope.id,
            "-payload",
            case.envelope.payload,
            "-audit-event-id",
            case.envelope.audit_event_id,
            "-project-id",
            case.envelope.project_id,
            "-doctrine",
            case.doctrine,
            "-lane",
            case.envelope.retrieval_lane.value,
            "-confidence",
            f"{case.envelope.confidence:.10g}",
        ]
        + (
            ["-expiration", case.envelope.expiration.isoformat()]
            if case.envelope.expiration is not None
            else []
        ),
        check=False,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"Go oracle invocation failed (exit={proc.returncode}): "
            f"stdout={proc.stdout!r} stderr={proc.stderr!r}"
        )
    return proc.stdout.rstrip("\n")


                                                                             
                         
                                                                             


def _python_footnote_for(case: _ParityCase) -> str:
    """Render JUST the per-citation Go-format footnote via the Python registry.

    Wrapper-level fallback emits N citations separated by ``\\n\\n``;
    when N=1, the output IS the per-citation footnote (no separator). We
    construct a 1-citation wrapper so the comparison is direct.
    """
    wrapper = AugmentationResult(
        request_id="req-parity",
        session_id="sess-parity",
        doctrine=case.doctrine,
        project_id=case.envelope.project_id,
        citations=[case.envelope],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:parity",
        audit_event_id="evt-parity-wrapper",
    )
    reg = RendererRegistry()
    register_default_renderers(reg)
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        result = reg.dispatch(wrapper, Platform.MARKDOWN_FALLBACK)
    output = result.output
    assert isinstance(output, str)
    return output


                                                                             
              
                                                                             


@pytest.mark.parametrize("case", _PARITY_CASES, ids=lambda c: c.name)
def test_python_markdown_fallback_matches_go_format(case: _ParityCase) -> None:
    """The Python wrapper-level fallback output for a single-citation wrapper
    is byte-exact equal to the Go ``renderFootnote`` output for the same
    envelope + session.

    Mode 1 (oracle): when ``bin/zen-markdown-fallback-golden`` is present,
    invoke it as the source-of-truth + compare. Mode 2 (replica): use the
    inline-computed expected string from ``_make_case`` (algorithmic
    replica of ``renderFootnote``).

    Both modes must agree if the Go binary is present.
    """
    py = _python_footnote_for(case)
    if _go_golden_oracle_available():
        oracle = _invoke_go_oracle(case)
        assert oracle == case.expected_footnote, (
            f"replica vs oracle drift in case {case.name!r}: "
            f"oracle={oracle!r} replica={case.expected_footnote!r}"
        )
    assert py == case.expected_footnote, (
        f"python markdown fallback drift vs go format in case {case.name!r}:\n"
        f"PY  : {py!r}\n"
        f"WANT: {case.expected_footnote!r}\n"
        "Source-of-truth: internal/citation/markdown_fallback.go:77-109."
    )


def test_multiple_citations_separated_by_double_newline() -> None:
    """Multi-citation wrapper emits per-citation footnotes joined by ``\\n\\n``.

    Go substrate renders ONE envelope per ``Render`` call; the Python
    wrapper-level fallback iterates over ``result.citations`` and joins
    the Go-format footnotes with ``\\n\\n`` so embedded markdown viewers
    treat each as a separate footnote definition.
    """
    cases = _PARITY_CASES[:2]
    wrapper = AugmentationResult(
        request_id="req-multi",
        session_id="sess-multi",
        doctrine=cases[0].doctrine,
        project_id="p",
        citations=[c.envelope for c in cases],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:multi",
        audit_event_id="evt-multi-wrapper",
    )
    reg = RendererRegistry()
    register_default_renderers(reg)
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        result = reg.dispatch(wrapper, Platform.MARKDOWN_FALLBACK)
    out = result.output
    assert isinstance(out, str)
                                                                        
                                                                     
                                                                      
                                                                     
                                            
    expected_blocks: list[str] = []
    for c in cases:
                                                                      
                                                                           
                                                           
        audit_url = f"zen://audit/{c.envelope.audit_event_id}"
        expected = (
            f"[^{c.envelope.id}]\n\n"
            f"[^{c.envelope.id}]: {c.envelope.payload}"
            f" ([{audit_url}]({audit_url}); "
            f"project={c.envelope.project_id}; "
            f"doctrine={wrapper.doctrine}; "
            f"lane={c.envelope.retrieval_lane.value}; "
            f"conf={c.envelope.confidence:.2f})"
        )
        expected_blocks.append(expected)
    expected_full = "\n\n".join(expected_blocks)
    assert out == expected_full, (
        f"multi-citation wrapper drift:\nGOT : {out!r}\nWANT: {expected_full!r}"
    )


def test_zero_citations_emits_placeholder_unchanged() -> None:
    """The empty-citations placeholder (``*(no citations)*``) is wrapper-
    level operator-friendly behaviour; Go substrate errors on a nil envelope
    and never emits an empty placeholder. The Python wrapper convention
    survives unchanged after the parity fix.
    """
    wrapper = AugmentationResult(
        request_id="req-empty",
        session_id="sess-empty",
        doctrine="capa-firewall",
        project_id="p",
        citations=[],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:empty",
        audit_event_id="evt-empty-wrapper",
    )
    reg = RendererRegistry()
    register_default_renderers(reg)
    result = reg.dispatch(wrapper, Platform.MARKDOWN_FALLBACK)
    assert result.output == "*(no citations)*"


def test_oracle_binary_documented_in_repo_when_missing() -> None:
    """If the Go oracle binary is missing, the in-Python replica must agree
    with the documented ``renderFootnote`` algorithm anyway — but we surface
    a skip marker via informational pytest output so operators know to run
    ``make`` first when chasing a real parity bug.
    """
    if _go_golden_oracle_available():
                                             
        return
                                                                  
                                                                   
                                                                      
                          
    pytest.skip(
        f"Go parity oracle not built (run `make bin/zen-markdown-fallback-golden` to "
        f"enable cross-language verification). Expected at {_GO_GOLDEN_BIN}. "
        "Replica-only mode remains active and verifies the algorithm."
    )


                                                                             
                                                                     
                                                                             


def test_parity_cases_cover_doctrine_x_lane_x_expiration_dimensions() -> None:
    """Sanity: the fixture set spans all three doctrines + multiple lanes +
    both expiration-set and expiration-None branches.

    Anchors the test surface: future maintenance can extend the
    ``_PARITY_CASES`` list and rely on this guard catching gaps.
    """
    doctrines: set[str] = {c.doctrine for c in _PARITY_CASES}
    lanes: set[str] = {c.envelope.retrieval_lane.value for c in _PARITY_CASES}
    expiration_set: set[bool] = {c.envelope.expiration is not None for c in _PARITY_CASES}
    assert doctrines >= {"max-scope", "default", "capa-firewall"}
    assert len(lanes) >= 3, lanes
    assert expiration_set == {True, False}


                                                                            
_: Any = None
