# 0006: Scripted UI verification in the repository

## Context

ADR 0004 kept axe-core and the Nu HTML Checker as ad hoc checks outside the repo. v1 requires them on every screen, in light and dark mode, before each merge, plus a page-weight budget.

## Decision

- `scripts/verify-ui.sh` (`make verify-ui`) starts `make demo` on a free port and drives headless Chromium through `playwright-core` (`scripts/verify-ui.mjs`): axe-core injected with `page.evaluate`, 390 px viewport, light and dark; server HTML saved for `vnu.jar`; group page weight summed as gzip -9.
- Tools are dev-only, pinned (playwright-core, axe-core, vnu-jar through npm) and installed once into the main checkout's `bin/tools/` (git-ignored), browsers included.
- Not part of `make check`: it needs Node, Java and about 10 s; it runs before each merge.

## Consequences

- Repeatable checks without driving the interactive Playwright browser; that one stays for visual review.
- Node and Java are needed on the development machine, never at runtime.
