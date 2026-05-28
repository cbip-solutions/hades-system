# SPDX-License-Identifier: MIT
# plugin/hades/renderers/voice_citation.py
"""TTS-ready voice renderer for citation envelopes (HADES design stage task)."""

from __future__ import annotations

import re

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)

# Shell metacharacters stripped from TTS output (defence-in-depth).
SHELL_METACHARS = "$`;|&<>\\\n\r"
_SHELL_METACHARS_RE = re.compile(r"[\$`;|&<>\\\n\r]")
# Markdown formatting chars stripped (TTS mispronunciation).
_MARKDOWN_FORMATTING_RE = re.compile(r"[*_~\[\]()#{}=+]")

# Per-citation word budget (~25 words → ~10 seconds at 150 wpm; 3 citations
# → 30 seconds total). Total TTS budget is _TOTAL_WORD_BUDGET (~75 words);
# per-citation budget scales 1/N within that envelope.
_PER_CITATION_WORD_BUDGET = 25
_TOTAL_WORD_BUDGET = 75

# Number-to-word maps for confidence percentage.
_TENS_WORDS = {
    0: "zero",
    10: "ten",
    20: "twenty",
    30: "thirty",
    40: "forty",
    50: "fifty",
    60: "sixty",
    70: "seventy",
    80: "eighty",
    90: "ninety",
}
_ONES_WORDS = {
    1: "one",
    2: "two",
    3: "three",
    4: "four",
    5: "five",
    6: "six",
    7: "seven",
    8: "eight",
    9: "nine",
}


def sanitize_for_tts(text: str) -> str:
    """Strip shell metacharacters + markdown formatting; collapse whitespace.

    Each removed char is replaced with a single space; runs of whitespace
    are then collapsed and trimmed. Result is TTS-safe (no chars that TTS
    engines mispronounce, no subprocess-injection vectors).
    """
    text = _SHELL_METACHARS_RE.sub(" ", text)
    text = _MARKDOWN_FORMATTING_RE.sub(" ", text)
    return re.sub(r"\s+", " ", text).strip()


def confidence_to_words(confidence: float) -> str:
    """Convert ``0.0..1.0`` confidence to a natural-language percentage phrase.

    Rounds to nearest integer percent; clamps to ``[0, 100]``. Returns
    ``"X percent"`` or compound form (``"ninety-four percent"``,
    ``"fifty-five percent"``).
    """
    pct = round(confidence * 100)
    pct = max(0, min(100, pct))
    if pct == 100:
        return "one hundred percent"
    if pct < 10:
        # 0..9: "zero percent" / "five percent" / etc.
        if pct == 0:
            return "zero percent"
        return f"{_ONES_WORDS[pct]} percent"
    tens = (pct // 10) * 10
    ones = pct % 10
    tens_word = _TENS_WORDS[tens]
    if ones == 0:
        return f"{tens_word} percent"
    return f"{tens_word}-{_ONES_WORDS[ones]} percent"


def pronounce_event_id(event_id: str) -> str:
    """Pronounce event ID with letters spelled out for clarity.

    Example: ``"evt-1234abcd"`` → ``"event 1234 A B C D"``.

    Numbers are grouped (TTS engines pronounce digit clusters
    acceptably). Letters are emitted letter-by-letter, uppercase form,
    separated by spaces (avoid TTS misreading ``a b c`` as a syllable).

    Hyphens, underscores, and other non-alphanumeric chars are dropped
    silently.

    Edge cases:
    - ``event_id`` starts with the literal prefix ``evt-``: that prefix
      is consumed and ``"event"`` is emitted (instead of the generic
      ``"event id"``). Case-insensitive.
    - ``event_id`` body contains no alphanumeric characters after the
      prefix strip (e.g., ``"evt-"`` alone, ``"evt-----"``, or
      ``"_-_"``): the function returns just the prefix
      (``"event"`` or ``"event id"``). The fallback is intentional —
      TTS that would otherwise emit an empty/whitespace string is more
      jarring than the prefix on its own.
    """
    body = event_id
    if body.lower().startswith("evt-"):
        body = body[4:]
        prefix = "event"
    else:
        prefix = "event id"

    parts: list[str] = [prefix]
    current_chunk = ""
    current_is_digit: bool | None = None
    for ch in body:
        if not ch.isalnum():
            # Flush any accumulated chunk then skip the non-alnum char.
            if current_chunk:
                parts.append(_chunk_to_voice(current_chunk, current_is_digit or False))
                current_chunk = ""
                current_is_digit = None
            continue
        is_digit = ch.isdigit()
        if current_is_digit is None:
            current_is_digit = is_digit
        if is_digit == current_is_digit:
            current_chunk += ch
        else:
            parts.append(_chunk_to_voice(current_chunk, current_is_digit))
            current_chunk = ch
            current_is_digit = is_digit
    if current_chunk:
        parts.append(_chunk_to_voice(current_chunk, current_is_digit or False))
    return " ".join(p for p in parts if p).strip()


def _chunk_to_voice(chunk: str, is_digit: bool) -> str:
    if is_digit:
        return chunk
    return " ".join(c.upper() for c in chunk if c.isalpha())


class VoiceCitationRenderer(Renderer):
    """Renders an `AugmentationResult` to TTS-ready plain text."""

    PLATFORM = Platform.VOICE

    def render(self, result: AugmentationResult) -> RenderResult:
        if not result.citations:
            return RenderResult(
                platform=Platform.VOICE,
                output="No citations available.",
                metadata=self._build_wrapper_meta(result),
                audit_event_ids=[],
            )

        n_citations = len(result.citations)
        per_citation_budget = max(8, _TOTAL_WORD_BUDGET // n_citations)
        per_citation_budget = min(per_citation_budget, _PER_CITATION_WORD_BUDGET)

        sentences: list[str] = []
        audit_ids: list[str] = []
        for citation in result.citations:
            sentences.append(self._build_sentence(citation, per_citation_budget))
            audit_ids.append(
                self.audit_anchor(
                    citation,
                    doctrine=result.doctrine,
                    rendered_at=result.emitted_at,
                )
            )

        full_text = " ".join(sentences)
        # Final defensive cap on total word count.
        words = full_text.split()
        if len(words) > _TOTAL_WORD_BUDGET:
            full_text = " ".join(words[:_TOTAL_WORD_BUDGET]) + "."

        return RenderResult(
            platform=Platform.VOICE,
            output=full_text,
            metadata=self._build_wrapper_meta(result),
            audit_event_ids=audit_ids,
        )

    def _build_sentence(self, citation: Envelope, word_budget: int) -> str:
        """Build one TTS-ready sentence per design contract"""
        payload_words = sanitize_for_tts(citation.payload).split()
        # Take first ~6 payload words; preserves identity without
        # overrunning budget.
        payload_phrase = " ".join(payload_words[:6])
        if len(payload_words) > 6:
            payload_phrase += " and so on"

        confidence_phrase = confidence_to_words(citation.confidence)
        event_phrase = pronounce_event_id(citation.audit_event_id)
        source = citation.source.value.replace("_", " ")

        sentence = (
            f"Citing {source}: {payload_phrase}, "
            f"{event_phrase}, confidence {confidence_phrase}."
        )
        # Per-citation budget cap (defensive: keeps the total TTS budget
        # stable even with adversarial payloads).
        words = sentence.split()
        if len(words) > word_budget:
            words = words[:word_budget]
            sentence = " ".join(words)
            if not sentence.endswith("."):
                sentence += "."
        return sanitize_for_tts(sentence)
