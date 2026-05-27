# Security Policy

## Reporting A Vulnerability

HADES system takes security reports seriously. Please do not open public issues
for vulnerabilities.

Use GitHub private vulnerability reporting at:

https://github.com/cbip-solutions/hades-system/security/advisories/new

If GitHub private reporting is unavailable, email `hades-dev@proton.me` with
the subject prefix `[hades-system SECURITY]`.

## Disclosure Timeline

The project follows a 90-day responsible-disclosure window from initial private
report. High-severity issues receive best-effort acceleration, but the 90-day
window remains the firm public-disclosure deadline.

## In Scope

- Code execution vulnerabilities.
- Authentication or authorization bypass.
- Privilege escalation.
- Sensitive information disclosure.
- Denial-of-service vulnerabilities.
- Dependency vulnerabilities that propagate into HADES system code.

## Out Of Scope

- Vulnerabilities in third-party or locally supplied sidecar implementations.
- Upstream dependency issues that should be reported directly upstream.
- User-specific configuration mistakes.
- Social-engineering, phishing, spam, or theoretical reports without a
  proof-of-concept.

## Recognition

Valid security reports may receive GHSA acknowledgment, CVE attribution when
applicable, and release-note credit unless the reporter asks to remain
anonymous.
