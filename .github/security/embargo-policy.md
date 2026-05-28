# 90-day embargo policy

hades-system follows a **90-day disclosure embargo** for private vulnerability reports:

## Timeline

- **Day 0**: Vulnerability reported privately via GHSA (or backup email `hades-dev@proton.me`).
- **Day 0-2**: Maintainer acknowledges receipt + initial triage classification.
- **Day 0-90**: Maintainer develops + tests fix; coordinates with reporter; publishes fix release.
- **Day 90**: Vulnerability details disclosed publicly regardless of fix status.

## Rationale

The 90-day window aligns with:

- [Google Project Zero convention](https://googleprojectzero.blogspot.com/p/vulnerability-disclosure-faq.html) (standard OSS practice).
- "Best-effort no-SLA" cadence for a single-maintainer project; no commitment to faster triage.
- Generous-but-firm timeline that respects reporter's desire for public disclosure if fix is delayed.

## Exceptions

- **Active exploitation in the wild**: maintainer may accelerate disclosure to inform affected users.
- **Critical severity (CVSS ≥ 9.0)**: maintainer commits to best-effort acceleration; 90-day window remains firm public-disclosure deadline.

## Out of scope

Optional local integration vulnerabilities follow their own disclosure path and are outside this public repository's GHSA scope.
