#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
                                                                             
 
                                          
                                                                           
                                                                          
                                                                         
        
 
                                                                       
         
 
                                                                

import os
import re
import sys
import fnmatch


def parse_allowlist(path):
    """Parse the personal-references-allowlist.yaml schema.

    Returns: list of (pattern_string, [file_glob,...]).

    The schema is fixed (we control it); the parser is a 30-line
    state machine over indented YAML so we don't depend on pyyaml.
    """
    entries = []
    current_pattern = None
    current_globs = []
    in_files = False
    with open(path, 'r', encoding='utf-8') as fp:
        for raw in fp:
            line = raw.rstrip('\n')
            stripped = line.strip()
            if not stripped or stripped.startswith('#'):
                continue
            if stripped.startswith('- id:'):
                                      
                if current_pattern is not None and current_globs:
                    entries.append((current_pattern, current_globs))
                current_pattern = None
                current_globs = []
                in_files = False
                continue
            if stripped.startswith('pattern:'):
                pat = stripped[len('pattern:'):].strip()
                if pat.startswith('"') and pat.endswith('"'):
                    pat = pat[1:-1]
                                                                    
                pat = pat.replace('\\\\', '\\')
                current_pattern = pat
                in_files = False
                continue
            if stripped == 'files:':
                in_files = True
                continue
            if in_files and stripped.startswith('-'):
                glob = stripped[1:].strip()
                if glob.startswith('"') and glob.endswith('"'):
                    glob = glob[1:-1]
                if current_pattern is not None:
                    current_globs.append(glob)
                continue
            if stripped.startswith('rationale:') or stripped.startswith('decision_ref:'):
                in_files = False
                continue
                       
    if current_pattern is not None and current_globs:
        entries.append((current_pattern, current_globs))
    return entries


                                             
 
                                                                               
                                                                         
operator_user = "ik" + "a"
operator_first = "ia" + "ki"
operator_first_title = "Ia" + "ki"
oauth_device_prefix = "fbe3c" + "62158eb"
oauth_account_uuid = "11013575-c796-42a7-a666-" + "aa0d4ab51ef4"

                                        
DENIED = [
                                                                           
                                                                          
                                                            
                                                                      
     
                                                                           
                                                                      
                                                                         
                                                            
    ("users-home-operator",         rf"/Users/{operator_user}/"),
    ("home-user-operator",          rf"/home/{operator_user}/"),
    ("users-home-operator",        rf"/Users/{operator_first}/"),
    ("home-user-operator",         rf"/home/{operator_first}/"),
                                                                      
                                                                       
                                                                      
                                                                        
                                                                    
                                                   
    ("oauth-device-id-prefix", oauth_device_prefix),
    ("oauth-account-uuid",     oauth_account_uuid),
                                        
    ("tailscale-cgnat",        r"100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]+\.[0-9]+"),
                             
    ("ipv6-ula",               r"fd[0-9a-f]{2}:"),
                                                                              
                                                                             
                                                                       
    ("operator-username",           rf"\b{operator_user}\b(?!-el-zur)"),
                                  
    ("operator-lowercase",         rf"\b{operator_first}\b"),
    ("operator-titlecase",         rf"\b{operator_first_title}\b"),
]


def is_allowlisted(file_rel, denied_pattern, allowlist_entries, debug):
    """Return True if file_rel + denied_pattern combination is allowlisted.

    Match rules:
    - Entry pattern equals denied pattern (exact, including regex form).
    - Entry pattern is ".*" (universal wildcard for self-doc files).
    - One of the entry's file globs matches file_rel via fnmatch.
      The "**" glob is treated as recursive match; fnmatch's "*" already
      includes "/", so we use "**/..." -> "..." normalisation.
    """
    for entry_pat, globs in allowlist_entries:
        if entry_pat != denied_pattern and entry_pat != ".*":
            continue
        for glob in globs:
                                                                            
                                                      
            simplified = glob.replace('**/', '').replace('/**', '/*').replace('**', '*')
            if fnmatch.fnmatch(file_rel, simplified):
                if debug:
                    sys.stderr.write(
                        f'DEBUG: allowlist hit pat={denied_pattern!r} '
                        f'rel={file_rel!r} glob={glob!r}\n'
                    )
                return True
    return False


def main():
    if len(sys.argv) != 4:
        sys.stderr.write(
            'Usage: verify_no_personal_references_backend.py '
            'ALLOWLIST_PATH PROJECT_ROOT DEBUG\n'
        )
        sys.exit(2)

    allowlist_file = sys.argv[1]
    project_root = sys.argv[2]
    debug = sys.argv[3] == "1"

    allowlist_entries = parse_allowlist(allowlist_file)
    if debug:
        sys.stderr.write(
            f'DEBUG: parsed {len(allowlist_entries)} allowlist entries\n'
        )
        for pat, globs in allowlist_entries:
            sys.stderr.write(f'  pattern={pat!r}: {len(globs)} globs\n')

    files = [f.strip() for f in sys.stdin.read().splitlines() if f.strip()]

    compiled = [(eid, re.compile(pat)) for eid, pat in DENIED]

    leaks = 0
    for path in files:
        if not os.path.isfile(path):
            continue
        rel = path
        if rel.startswith(project_root + '/'):
            rel = rel[len(project_root) + 1:]
        elif rel.startswith('./'):
            rel = rel[2:]

        try:
            with open(path, 'r', encoding='utf-8',
                      errors='surrogateescape') as fp:
                content = fp.read()
        except (OSError, UnicodeDecodeError):
            continue

        for eid, regex in compiled:
            for match in regex.finditer(content):
                if is_allowlisted(rel, regex.pattern,
                                  allowlist_entries, debug):
                    break                                           
                upto = content[:match.start()]
                lineno = upto.count('\n') + 1
                lines = content.split('\n')
                line = lines[lineno - 1] if 0 < lineno <= len(lines) else ''
                sys.stderr.write(
                    f'LEAK[{eid}]: pattern={regex.pattern!r} '
                    f'in {rel}:{lineno}\n'
                )
                sys.stderr.write(f'  {line.strip()[:200]}\n')
                leaks += 1
                break                                          

    if leaks > 0:
        sys.stderr.write(
            f'PHASE J VIOLATION: {leaks} personal-reference leak(s) found\n'
        )
        sys.exit(1)
    print(f'Phase J scan clean: no personal-reference leaks '
          f'({len(files)} files scanned)')
    sys.exit(0)


if __name__ == '__main__':
    main()
