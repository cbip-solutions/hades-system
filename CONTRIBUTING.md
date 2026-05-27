# Contributing To HADES system

Thank you for taking the time to improve HADES system. This project is strict
about correctness, reviewability, and release verification because it coordinates
autonomous development work across real repositories.

## Ground Rules

- Use conventional commits: `type(scope): subject`.
- Sign commits with DCO: `git commit -s`.
- Do not add generated-by-tool or co-author attribution lines.
- Keep changes scoped and reviewable.
- Add or update tests for behavior changes.
- Keep docs professional, current, and free of credentials, local machine paths,
  and unrelated project details.
- Do not add stubs, placeholder implementations, or open-ended TODO markers to
  production code.

## Local Checks

Run the focused checks for your change, then run the release gates before
opening a substantial pull request:

```bash
make build
make lint
make test
make verify-license-compliance
make verify-no-personal-references
make verify-no-task-context-comments
```

For documentation-only changes, also run:

```bash
make verify-no-personal-references
```

For code-comment or exported API changes, also run:

```bash
make verify-no-task-context-comments
```

## Pull Requests

Small fixes, typo corrections, and focused documentation improvements are the
easiest changes to review.

Larger changes may be declined, split, or deferred depending on maintainer
capacity and roadmap fit.

## Security

Do not open public issues for vulnerabilities. Follow [SECURITY.md](SECURITY.md)
and use GitHub Security Advisories for private vulnerability reports.

## Code Of Conduct

Participation in this project is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
