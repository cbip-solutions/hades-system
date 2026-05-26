#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Static lint: prompt-template /v1/* paths vs daemon registrations.

Plan 12 Phase E Stage 2 reviewer surfaced drift between the prompt
strings shipped in ``plugin/zen-swarm/commands/*.py`` and the daemon-side
``mux.HandleFunc`` registrations in ``internal/daemon/server.go`` (and
neighbouring ``*_routes.go`` helpers). Some referenced paths do not yet
exist daemon-side; some reference legacy paths that were renamed.

This lint walks every ``commands/*.py`` file, extracts ``/v1/<path>``
substrings, normalises path parameters (``{var}``, ``{var}/sub`` →
generic shape), and reports the set of paths that are NOT registered in
the daemon. Paths annotated with a ``# Plan N — pending endpoint
registration`` comment within 3 lines of the reference are allowlisted
(documented future-Plan dependency).

Exit codes:

  0 — no drift; every referenced /v1/* path is registered (or annotated)
  1 — drift detected; report printed to stdout

Usage:

    python3 scripts/lint_prompt_endpoints.py
    python3 scripts/lint_prompt_endpoints.py --plugin-dir plugin/zen-swarm/commands

Wired into ``make lint`` via ``lint-prompt-endpoints``. Plan 12 Phase E
MAJOR-2 ships the lint + the 3 daemon endpoints (mcpgateway sub-paths,
augment/summary, hermes/probe) referenced by the F-panels.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

                                                                      
                                                                    
                                              
PATH_PATTERN = re.compile(r"/v1/[A-Za-z0-9_/${}-]+")

                                                                  
                                                                        
                                   
ROUTE_PATTERN = re.compile(r'"(?:GET |POST |PUT |DELETE |PATCH )?(/v1/[^"]+)"')


def normalize_path(path: str) -> str:
    """Normalize {var} + $SHELL → wildcard token to match daemon's
    Go-1.22 mux pattern (which writes ``{name}`` per segment).

    Examples:
        /v1/swarms/{id}/archive          → /v1/swarms/{}/archive
        /v1/swarms/$SWARM_ID/cleanup     → /v1/swarms/{}/cleanup
        /v1/knowledge/{item_id}/promote  → /v1/knowledge/{}/promote
        /v1/swarms?feature={feature_name}→ /v1/swarms
    """
                                     
    for terminator in ("?", "#"):
        idx = path.find(terminator)
        if idx >= 0:
            path = path[:idx]
                                                
    out = []
    for seg in path.split("/"):
        if not seg:
            out.append(seg)
            continue
        if seg.startswith("{") and seg.endswith("}"):
            out.append("{}")
        elif seg.startswith("$"):
            out.append("{}")
        else:
            out.append(seg)
    return "/".join(out)


def collect_prompt_paths(plugin_dir: Path) -> dict[str, list[tuple[Path, int, str]]]:
    """Walk plugin/zen-swarm/commands/*.py, extract /v1/* paths.

    Returns a dict {normalized_path: [(file, lineno, raw_line), ...]}.
    Excludes paths preceded within 3 lines by a 'Plan N — pending
    endpoint registration' comment (allowlist).
    """
    found: dict[str, list[tuple[Path, int, str]]] = {}
    for path_file in sorted(plugin_dir.glob("*.py")):
        if path_file.name == "__init__.py":
            continue
        lines = path_file.read_text(encoding="utf-8").splitlines()
        for lineno, line in enumerate(lines, start=1):
            for match in PATH_PATTERN.finditer(line):
                raw = match.group(0)
                                                                        
                raw = raw.rstrip(",;:)\"'\\")
                                                                         
                                                                         
                                                                       
                                                                          
                start = max(0, lineno - 9)
                window = "\n".join(lines[start:lineno])
                if "pending endpoint registration" in window:
                    continue
                if "pending endpoint registration" in line:
                    continue
                norm = normalize_path(raw)
                found.setdefault(norm, []).append((path_file, lineno, line.strip()))
    return found


def collect_daemon_routes(daemon_file: Path) -> set[str]:
    """Walk daemon source files (server.go + neighbours), extract every
    registered /v1/* path, normalised. Tolerates the route list constants
    + per-route HandleFunc forms.
    """
    routes: set[str] = set()
    for candidate in [daemon_file, *daemon_file.parent.glob("*.go")]:
        if not candidate.is_file():
            continue
        try:
            text = candidate.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        for match in ROUTE_PATTERN.finditer(text):
            routes.add(normalize_path(match.group(1)))
    return routes


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--plugin-dir",
        default="plugin/hades/commands",
        help="Plugin commands directory (default: plugin/hades/commands)",
    )
    parser.add_argument(
        "--daemon-file",
        default="internal/daemon/server.go",
        help="Daemon server source file (default: internal/daemon/server.go)",
    )
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress per-drift detail; print only the count + paths",
    )
    args = parser.parse_args()

    plugin_dir = Path(args.plugin_dir)
    daemon_file = Path(args.daemon_file)
    if not plugin_dir.is_dir():
        print(f"ERROR: plugin dir not found: {plugin_dir}", file=sys.stderr)
        return 2
    if not daemon_file.is_file():
        print(f"ERROR: daemon file not found: {daemon_file}", file=sys.stderr)
        return 2

    prompt_paths = collect_prompt_paths(plugin_dir)
    daemon_routes = collect_daemon_routes(daemon_file)

    drift: dict[str, list[tuple[Path, int, str]]] = {}
    for norm, refs in prompt_paths.items():
        if norm in daemon_routes:
            continue
                                                                             
                                                                     
                                                              
        matched = False
        for route in daemon_routes:
            if route == norm:
                matched = True
                break
                                                          
            if route.endswith("/") and norm.startswith(route):
                matched = True
                break
                                                                  
            if norm.startswith(route + "/{}"):
                matched = True
                break
            if route.startswith(norm + "/"):
                matched = True
                break
        if not matched:
            drift[norm] = refs

    print(f"Scanned {len(prompt_paths)} unique /v1/* paths in {plugin_dir}/")
    print(f"Daemon registers {len(daemon_routes)} unique /v1/* routes")
    if not drift:
        print("OK: every referenced path has a registered counterpart")
        return 0

    print(f"DRIFT: {len(drift)} unregistered paths referenced in prompts:\n")
    for norm, refs in sorted(drift.items()):
        print(f"  {norm}")
        if args.quiet:
            continue
        for path_file, lineno, line in refs[:3]:
            print(f"    {path_file}:{lineno}: {line[:120]}")
        if len(refs) > 3:
            print(f"    ... and {len(refs) - 3} more references")
        print()
    print("Resolve each drift by EITHER:")
    print("  (a) registering the endpoint on the daemon (preferred), OR")
    print("  (b) annotating the prompt with `# Plan N — pending endpoint registration`")
    print("     within 3 lines BEFORE the curl reference.")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
