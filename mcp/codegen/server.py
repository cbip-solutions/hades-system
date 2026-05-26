# SPDX-License-Identifier: MIT
"""Zen-Swarm codegen MCP server.

The single path through which source code is produced. Routes requests to
DeepSeek / GLM / Kimi / local-MLX based on policy. Writes files itself
(subagents have Write/Edit denied on source files).

Tools:
  - codegen_implement(spec, target_file, language, context, criteria, hint)
  - codegen_fix(file_path, error, related_files)
  - codegen_review(diff, original_provider)
  - codegen_test_gen(file_path, focus, framework)
  - codegen_refactor(file_path, instruction, constraints)
  - codegen_health()
  - codegen_write_artifact(path, content)   # for non-source artifacts (status.json, notes)

Run via:
  python server.py
or as configured in plugin/plugin.json mcp_servers.codegen.
"""
from __future__ import annotations

import json
import os
import sys
import time
import tomllib
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Literal

import httpx
from fastmcp import FastMCP

                                                                               
        
                                                                               

CONFIG_PATH = Path(
    os.environ.get("CODEGEN_CONFIG", str(Path(__file__).parent / "codegen.toml"))
)
SECRETS_PATH = Path(
    os.environ.get("CODEGEN_SECRETS",
                   str(Path.home() / ".config/zen-swarm/secrets.env"))
)
LOG_DIR = Path(
    os.environ.get("ZEN_LOG_DIR", str(Path.home() / ".local/share/zen-swarm"))
)
LOG_DIR.mkdir(parents=True, exist_ok=True)
LOG_FILE = LOG_DIR / "codegen.log"

if not CONFIG_PATH.exists():
    print(f"codegen.toml not found at {CONFIG_PATH}", file=sys.stderr)
    sys.exit(2)

CFG = tomllib.loads(CONFIG_PATH.read_text())

                                                                              
SECRETS: dict[str, str] = {}
if SECRETS_PATH.exists():
    for line in SECRETS_PATH.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        SECRETS[k.strip()] = v.strip().strip('"').strip("'")

                                                                               
                  
                                                                               

class ProviderError(RuntimeError):
    pass


def _post_chat(base_url: str, api_key: str, model: str,
               messages: list[dict], timeout: float = 180.0,
               temperature: float = 0.2, max_tokens: int = 8192) -> dict:
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    payload = {
        "model": model,
        "messages": messages,
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    r = httpx.post(f"{base_url}/chat/completions",
                   json=payload, headers=headers, timeout=timeout)
    if r.status_code != 200:
        raise ProviderError(f"{base_url} HTTP {r.status_code}: {r.text[:300]}")
    return r.json()


def _extract_text(resp: dict) -> str:
    return resp["choices"][0]["message"]["content"]


def call_provider(provider_id: str, messages: list[dict],
                  temperature: float = 0.2, max_tokens: int = 8192) -> dict:
    p = CFG["providers"][provider_id]
    api_key = SECRETS.get(p.get("api_key_env", ""), "")
    base_url = p["base_url"]
    model = p["model"]

    t0 = time.time()
    try:
        resp = _post_chat(base_url, api_key, model, messages,
                          timeout=p.get("timeout_s", 180.0),
                          temperature=temperature,
                          max_tokens=max_tokens)
    except (httpx.HTTPError, ProviderError) as e:
        return {"ok": False, "error": str(e), "provider": provider_id,
                "latency_ms": int((time.time() - t0) * 1000)}

    text = _extract_text(resp)
    usage = resp.get("usage", {})
    return {
        "ok": True,
        "provider": provider_id,
        "model": model,
        "text": text,
        "in_tokens": usage.get("prompt_tokens"),
        "out_tokens": usage.get("completion_tokens"),
        "latency_ms": int((time.time() - t0) * 1000),
    }


                                                                               
                
                                                                               

TaskKind = Literal["implement", "fix", "review", "test_gen", "refactor"]


def route(kind: TaskKind, *, hint: str | None = None,
          privacy_required: bool = False,
          estimated_input_tokens: int = 0,
          complexity: Literal["low", "med", "high"] = "med",
          forbid_provider: str | None = None) -> list[str]:
    """Return an ordered list of provider ids to try (primary + fallbacks)."""
    fallbacks = CFG["routing"]["fallbacks"]
    long_ctx_threshold = CFG["routing"].get("long_context_threshold", 50000)
    default = CFG["routing"]["default"]

    primary: str

    if hint and hint in CFG["providers"] and not privacy_required:
        primary = hint
    elif privacy_required:
        primary = "local-32b"
    elif estimated_input_tokens > long_ctx_threshold:
        primary = "kimi"
    elif kind == "test_gen":
        primary = "local-14b"
    elif complexity == "high":
        primary = CFG["routing"].get("high_complexity", "deepseek")
    else:
        primary = default

    chain = [primary] + fallbacks.get(primary, [])
    if forbid_provider:
        chain = [p for p in chain if p != forbid_provider]
                             
    seen, out = set(), []
    for p in chain:
        if p not in seen and p in CFG["providers"]:
            out.append(p); seen.add(p)
    return out


def log_call(*, change: str, task: str, tool: str, provider: str,
             in_tok: int | None, out_tok: int | None,
             cost_usd: float, latency_ms: int, ok: bool, note: str = "") -> None:
    rec = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "change": change, "task": task, "tool": tool,
        "provider": provider, "in_tokens": in_tok, "out_tokens": out_tok,
        "cost_usd": cost_usd, "latency_ms": latency_ms,
        "ok": ok, "note": note,
    }
    with LOG_FILE.open("a") as fh:
        fh.write(json.dumps(rec) + "\n")


def estimate_cost(provider: str, in_tok: int | None, out_tok: int | None) -> float:
    p = CFG["providers"][provider]
    in_rate = p.get("price_in_per_million", 0.0)
    out_rate = p.get("price_out_per_million", 0.0)
    in_c = (in_tok or 0) / 1_000_000 * in_rate
    out_c = (out_tok or 0) / 1_000_000 * out_rate
    return round(in_c + out_c, 6)


def call_with_fallback(*, kind: TaskKind, messages: list[dict],
                       hint: str | None, privacy_required: bool,
                       estimated_input_tokens: int,
                       complexity: str,
                       forbid_provider: str | None,
                       change: str, task: str, tool: str,
                       temperature: float = 0.2,
                       max_tokens: int = 8192) -> dict:
    chain = route(kind, hint=hint, privacy_required=privacy_required,
                  estimated_input_tokens=estimated_input_tokens,
                  complexity=complexity, forbid_provider=forbid_provider)
    if not chain:
        raise ProviderError("no providers available after policy + diversity filter")

    last_err = None
    for provider in chain:
        result = call_provider(provider, messages,
                               temperature=temperature, max_tokens=max_tokens)
        cost = estimate_cost(provider,
                             result.get("in_tokens"), result.get("out_tokens"))
        log_call(change=change, task=task, tool=tool, provider=provider,
                 in_tok=result.get("in_tokens"), out_tok=result.get("out_tokens"),
                 cost_usd=cost, latency_ms=result.get("latency_ms", 0),
                 ok=result["ok"], note=result.get("error", ""))
        if result["ok"]:
            result["cost_usd"] = cost
            return result
        last_err = result.get("error")

    raise ProviderError(f"all providers failed; last error: {last_err}")


                                                                               
                   
                                                                               

def strip_code_fences(text: str, language: str | None = None) -> str:
    t = text.strip()
    if not t.startswith("```"):
        return t
    lines = t.splitlines()
                      
    lines = lines[1:]
                                
    if lines and lines[-1].strip().startswith("```"):
        lines = lines[:-1]
    return "\n".join(lines)


def write_safely(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


                                                                               
            
                                                                               

mcp = FastMCP("zen-swarm-codegen")


@mcp.tool()
def codegen_health() -> dict:
    """Probe each configured provider and report availability."""
    out = {"providers": {}, "config_path": str(CONFIG_PATH)}
    for pid, p in CFG["providers"].items():
        if p.get("kind") == "local":
            try:
                r = httpx.get(f"{p['base_url'].rstrip('/v1')}/health", timeout=5.0)
                out["providers"][pid] = {"ok": r.status_code == 200,
                                         "info": r.json() if r.status_code == 200 else r.text[:200]}
            except Exception as e:
                out["providers"][pid] = {"ok": False, "error": str(e)}
        else:
            api_key = SECRETS.get(p.get("api_key_env", ""), "")
            out["providers"][pid] = {"ok": bool(api_key),
                                     "note": "key configured" if api_key else "missing API key"}
    return out


@mcp.tool()
def codegen_implement(spec: str, target_file: str, language: str,
                      context: str = "", criteria: str = "",
                      hint: str | None = None,
                      privacy_required: bool = False,
                      complexity: str = "med",
                      change_id: str = "adhoc",
                      task_id: str = "adhoc") -> dict:
    """Generate code for a target file from a spec. Writes the file on success.

    Returns {provider, cost_usd, written_path, lines, summary}.
    """
    sys_prompt = (
        "You are a senior engineer. Output ONLY the file's complete contents, "
        "no prose, no fences except language-tagged code fence wrapping the file. "
        "If tests are mentioned in the criteria, include them as a separate output "
        "between '<<<FILE: <test_path>>>>' markers."
    )
    user = (
        f"## Spec\n{spec}\n\n"
        f"## Target file\n`{target_file}` ({language})\n\n"
        f"## Context\n{context}\n\n"
        f"## Acceptance criteria\n{criteria}\n"
    )

    result = call_with_fallback(
        kind="implement",
        messages=[{"role": "system", "content": sys_prompt},
                  {"role": "user", "content": user}],
        hint=hint,
        privacy_required=privacy_required,
        estimated_input_tokens=len(user) // 4,
        complexity=complexity,
        forbid_provider=None,
        change=change_id, task=task_id, tool="implement",
        max_tokens=12000,
    )

    code = strip_code_fences(result["text"], language)
    target = Path(target_file)
    write_safely(target, code)

    return {
        "ok": True,
        "provider": result["provider"],
        "model": result["model"],
        "cost_usd": result["cost_usd"],
        "latency_ms": result["latency_ms"],
        "written_path": str(target),
        "lines": code.count("\n") + 1,
    }


@mcp.tool()
def codegen_fix(file_path: str, error: str, related_files: list[str] | None = None,
                hint: str | None = None,
                change_id: str = "adhoc", task_id: str = "adhoc") -> dict:
    """Patch a file given an error message and related context.

    Loads the file, sends it with the error to the chosen provider, writes
    back the patched contents.
    """
    target = Path(file_path)
    if not target.exists():
        return {"ok": False, "error": f"file not found: {file_path}"}

    related_blob = ""
    for rel in (related_files or []):
        p = Path(rel)
        if p.exists() and p.is_file():
            related_blob += f"\n### {rel}\n```\n{p.read_text(errors='replace')[:4000]}\n```\n"

    sys_prompt = (
        "You are fixing a bug. Output ONLY the complete corrected file contents "
        "wrapped in a single code fence. Do not narrate."
    )
    user = (
        f"## Error\n{error}\n\n"
        f"## File: {file_path}\n```\n{target.read_text()}\n```\n\n"
        f"## Related\n{related_blob}\n"
    )

    result = call_with_fallback(
        kind="fix",
        messages=[{"role": "system", "content": sys_prompt},
                  {"role": "user", "content": user}],
        hint=hint, privacy_required=False,
        estimated_input_tokens=len(user) // 4,
        complexity="med",
        forbid_provider=None,
        change=change_id, task=task_id, tool="fix",
        max_tokens=12000,
    )

    code = strip_code_fences(result["text"])
    write_safely(target, code)
    return {"ok": True, "provider": result["provider"], "model": result["model"],
            "cost_usd": result["cost_usd"], "latency_ms": result["latency_ms"],
            "written_path": str(target), "lines": code.count("\n") + 1}


@mcp.tool()
def codegen_review(diff: str, original_provider: str,
                   change_id: str = "adhoc", task_id: str = "adhoc") -> dict:
    """Adversarial review of a diff. Forces a different provider than the author."""
    sys_prompt = (
        "You are reviewing code you did not write. Return JSON only: "
        '[{"severity":"critical|advisory|nit","title":"...","rationale":"..."}]. '
        "Empty array if clean."
    )
    user = f"## Diff\n```diff\n{diff[:60000]}\n```"

    result = call_with_fallback(
        kind="review",
        messages=[{"role": "system", "content": sys_prompt},
                  {"role": "user", "content": user}],
        hint=None, privacy_required=False,
        estimated_input_tokens=len(user) // 4,
        complexity="med",
        forbid_provider=original_provider,
        change=change_id, task=task_id, tool="review",
        max_tokens=4000,
    )

    raw = strip_code_fences(result["text"])
    try:
        findings = json.loads(raw)
    except json.JSONDecodeError:
        findings = [{"severity": "advisory", "title": "non-JSON review",
                     "rationale": raw[:400]}]
    return {"ok": True, "provider": result["provider"],
            "cost_usd": result["cost_usd"], "findings": findings}


@mcp.tool()
def codegen_test_gen(file_path: str, focus: str = "", framework: str = "auto",
                     change_id: str = "adhoc", task_id: str = "adhoc") -> dict:
    """Generate a test file for an existing source file. Prefers local."""
    target = Path(file_path)
    if not target.exists():
        return {"ok": False, "error": f"file not found: {file_path}"}

                               
    if framework == "auto":
        if target.suffix == ".py":
            framework = "pytest"
            test_path = target.with_name(f"test_{target.stem}.py")
        elif target.suffix in {".ts", ".tsx", ".js", ".jsx"}:
            framework = "vitest"
            test_path = target.with_name(f"{target.stem}.test{target.suffix}")
        else:
            return {"ok": False, "error": f"no auto framework for {target.suffix}"}
    else:
        test_path = target.with_name(f"{target.stem}.test{target.suffix}")

    sys_prompt = (
        f"Generate {framework} tests. Output ONLY the test file contents. "
        "Cover the happy path, at least one edge case, and one failure mode."
    )
    user = (
        f"## Source: {file_path}\n```\n{target.read_text()}\n```\n\n"
        f"## Focus\n{focus or 'public API'}\n"
    )

    result = call_with_fallback(
        kind="test_gen",
        messages=[{"role": "system", "content": sys_prompt},
                  {"role": "user", "content": user}],
        hint="local-14b", privacy_required=False,
        estimated_input_tokens=len(user) // 4,
        complexity="low",
        forbid_provider=None,
        change=change_id, task=task_id, tool="test_gen",
        max_tokens=6000,
    )

    code = strip_code_fences(result["text"])
    write_safely(test_path, code)
    return {"ok": True, "provider": result["provider"],
            "cost_usd": result["cost_usd"], "written_path": str(test_path),
            "lines": code.count("\n") + 1}


@mcp.tool()
def codegen_refactor(file_path: str, instruction: str,
                     constraints: str = "",
                     hint: str | None = None,
                     change_id: str = "adhoc", task_id: str = "adhoc") -> dict:
    """Restructure a file according to an instruction without changing behaviour."""
    target = Path(file_path)
    if not target.exists():
        return {"ok": False, "error": f"file not found: {file_path}"}

    sys_prompt = (
        "You are refactoring. Behaviour MUST be preserved. Output ONLY the "
        "complete refactored file in a single code fence."
    )
    user = (
        f"## Instruction\n{instruction}\n\n"
        f"## Constraints\n{constraints}\n\n"
        f"## File: {file_path}\n```\n{target.read_text()}\n```\n"
    )

    result = call_with_fallback(
        kind="refactor",
        messages=[{"role": "system", "content": sys_prompt},
                  {"role": "user", "content": user}],
        hint=hint, privacy_required=False,
        estimated_input_tokens=len(user) // 4,
        complexity="med",
        forbid_provider=None,
        change=change_id, task=task_id, tool="refactor",
        max_tokens=12000,
    )

    code = strip_code_fences(result["text"])
    write_safely(target, code)
    return {"ok": True, "provider": result["provider"],
            "cost_usd": result["cost_usd"], "written_path": str(target),
            "lines": code.count("\n") + 1}


@mcp.tool()
def codegen_write_artifact(path: str, content: str) -> dict:
    """Write a non-source artifact (status.json, notes, spec deltas).

    This is the only allowed way for swarm-coder subagents to write to disk
    without going through a code-generating provider. Refuses to write into
    common source directories.
    """
    forbidden_prefixes = ("src/", "lib/", "app/", "internal/")
    p = Path(path)
    rel = str(p)
    if any(rel.startswith(x) for x in forbidden_prefixes):
                                                             
        if "openspec/" not in rel and ".notes." not in rel:
            return {"ok": False, "error": "refused: source directory; use codegen_implement"}
    write_safely(p, content)
    return {"ok": True, "written_path": rel, "bytes": len(content)}


if __name__ == "__main__":
    mcp.run()
