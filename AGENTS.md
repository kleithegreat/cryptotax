# Agent Guidelines

Read this file before starting any task in this repository.

## Repository Purpose

`cryptotax` is a crypto tax report generator with:

- Go plumbing in `./go` for ingestion, normalization, transfer matching, pricing helpers, and audit tooling
- Haskell financial core in `./haskell` for FIFO lot accounting and final tax-oriented output
- a flake-native workflow at the repository root

The repository is optimized for tax accuracy, evidence retention, and explicit handling of unsupported cases.

## Core Rules

- Do not reintroduce a Makefile.
- Prefer flake-native workflows.
- Do not leak secrets in defaults, logs, docs, fixtures, or help text.
- Do not silently invent tax behavior.
- Do not silently invent asset identity, symbols, labels, or USD valuations.
- Unsupported or partially modeled behavior must be surfaced, not hidden.
- Keep changes small, coherent, and production-quality.
- When changing classification or semantics, add focused regression coverage.
- Do not treat current-behavior fixtures as proof of tax correctness unless the docs explicitly say so.

## Support Tiers

Canonical tier definitions live in `docs/repo/SPEC.md`; this list is the working summary. When describing behavior, use these categories consistently:

- **Supported** — intentionally modeled and relied upon within current repo scope
- **Current-behavior-only** — frozen by regression tests or fixtures, but not yet a claim of tax correctness
- **Unsupported but surfaced** — preserved explicitly instead of dropped or guessed
- **Human-verified exemplar** — backed by source evidence recorded in the repo

If a behavior does not clearly fit one of these categories, document the ambiguity instead of overstating confidence.

## Before Making Changes

Read the relevant documentation first.

Documentation lives under `docs/` and is organized by domain. Each domain may contain:

- `SPEC.md` — authoritative design intent and contracts
- `ARCHITECTURE.md` — current implementation map
- `REVIEW.md` — known gaps, divergences, and follow-up work
- `QUIRKS.md` — non-obvious gotchas and debugging-derived knowledge

Read in this order:

1. relevant `SPEC.md`
2. relevant `ARCHITECTURE.md`
3. relevant `REVIEW.md`
4. relevant `QUIRKS.md`

If a domain does not yet have full docs, read the repo-wide docs under `docs/repo/` and the nearest related domain docs before changing code.

## Human vs Agent Decision Boundary

Agents should execute implementation, testing, validation, fixture updates, and documentation updates by default.

Defer to a human only for:

- changes that broaden or narrow a support claim
- changes to tax semantics or interpretation
- adoption of a new external source of truth
- schema changes that alter long-term repository contracts
- non-obvious heuristic expansions
- cases where multiple plausible interpretations remain and source evidence does not decide between them

Everything else should default to agent execution.

## Validation

Agents should run validation themselves and report the actual results.

Repository-wide validation:

```bash
nix develop -c bash -lc 'cd go && go test ./...'
nix build .#cli
nix build .#core
nix flake check   # runs the full Go test suite (checks.go-tests) and Haskell suite
```

Docs-only changes may skip builds when genuinely unnecessary, but the report must say so explicitly.

## Repo-Specific Expectations

### Nix

* Use supported flake outputs rather than ad hoc wrapper scripts.
* Do not reintroduce deleted shell-wrapper workflows as primary interfaces.
* If `vendorHash` changes, update it deliberately and report the final value.

### Go

* Reuse typed structures where possible.
* Prefer explicit typed helpers and subcommands over shell-script plumbing.
* Add or update focused tests when changing fetch, normalize, transfer-match, pricing, or audit behavior.
* Do not replace structured source identity with guessed display identity.

### Haskell

* Preserve exact arithmetic semantics.
* Do not introduce floating-point tax math.
* Keep or improve semantic coverage when changing lot, proceeds, basis, income, or expense handling.

## Tax-Accuracy Policy

This repository is trying to be accurate, not merely plausible.

* Do not collapse complex activity into simpler classifications unless that limitation is already documented and intentional.
* If handling is approximate, say so in code comments and docs.
* Unsupported or partially modeled cases should remain visible.
* Do not claim a real-wallet case is verified unless the evidence is present in the repository docs or explicitly provided by the user.

High-risk areas include:

* EVM swaps and multi-leg contract activity
* bridges and own-wallet cross-chain transfers
* Solana multi-leg transactions and mint identity
* Hyperliquid perpetuals and funding
* asset identity / symbol resolution / USD valuation gaps

## Audit Tooling Policy

Prefer Go-based audit tooling integrated into the CLI.

Audit tools should support:

* capture of normalized output
* filtering by tx id / wallet / asset / raw type / timestamp range
* stable machine-readable summaries
* surfacing unresolved and suspicious cases early

Audit tooling is for evidence capture and review support. It does not, by itself, prove semantic or tax correctness.

## After Making Changes

Update any affected documentation. This is required.

* If behavior changed relative to a `SPEC.md`, either update the implementation or intentionally update the spec.
* If implementation structure changed, update the relevant `ARCHITECTURE.md`.
* If an issue in `REVIEW.md` was resolved, remove or revise it.
* If a workaround or gotcha changed, update `QUIRKS.md`.
* If you discovered a new issue, add it to the relevant `REVIEW.md`.
* If you discovered a non-obvious operational gotcha, add it to the relevant `QUIRKS.md`.

Do not leave docs contradicting code.

## Reporting Format

End work with this structure:

1. Verified current behavior
2. What changed
3. Why it changed
4. Validation results
5. Remaining unsupported or unresolved behavior
6. Human decisions still needed

Do not end routine work with “commands to run next” unless the human explicitly asked for them.