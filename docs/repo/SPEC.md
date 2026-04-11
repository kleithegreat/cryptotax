# Repository Spec

This document is the repository-wide source of truth for how `cryptotax` should behave and how agents should work in the repository.

## Purpose

`cryptotax` is a crypto tax report generator with:

- Go plumbing in `./go` for source ingestion, normalization, and audit tooling
- Haskell financial core in `./haskell` for lot accounting and final tax-output generation
- a flake-native workflow at the repository root

The repository is optimized for tax accuracy, evidence retention, and explicit handling of unsupported cases.

## Authoritative design rules

1. **Do not silently invent tax behavior.**
   Unsupported or partially modeled cases must be surfaced rather than hidden.
2. **Do not silently invent identity or valuation data.**
   Asset identity, symbols, labels, and USD values may only come from source-backed data or an explicitly adopted mapping/valuation source.
3. **Preserve evidence.**
   Raw-source details should remain accessible long enough for audit review and human verification.
4. **Keep changes small and reviewable.**
   Favor narrow checkpoints over broad refactors.
5. **Prefer flake-native workflows.**
   Do not reintroduce a Makefile or wrapper-driven build flow.

## Support tiers

Every non-trivial behavior in this repository should be documented as one of the following:

### Supported

The behavior is intentionally modeled and evidence-backed enough to rely on within the repository's current scope.

### Current-behavior-only

The behavior is frozen by regression tests or fixtures so it does not drift accidentally, but it is **not** yet a claim of tax correctness.

### Unsupported but surfaced

The repository does not yet know how to model the case fully, but it preserves the case explicitly instead of dropping it, flattening it into a misleading simpler case, or guessing.

### Human-verified exemplar

A real transaction or fixture with source evidence that anchors future implementation and regression work.

## Human vs agent decision boundary

Agents should execute implementation, tests, documentation updates, and validation by default.

Agents must defer to a human for:

- changes that broaden or narrow a support claim
- changes to tax semantics or interpretation
- adoption of a new external source of truth for pricing, identity, or reconciliation
- schema changes that alter long-term repository contracts
- cases where multiple plausible semantic interpretations remain and source evidence does not decide between them
- non-obvious heuristic expansions beyond already documented scope

Everything else should default to agent execution.

## Documentation contract

Documentation under `docs/` is organized by domain. Each domain may contain:

- `SPEC.md` — what the system should do in that domain
- `ARCHITECTURE.md` — what the code currently does in that domain
- `REVIEW.md` — known gaps between the spec and implementation, plus near-term work
- `QUIRKS.md` — operational gotchas and source-specific caveats

When a spec and code disagree, the spec wins by default unless a human intentionally updates the spec.

## Validation contract

Agents should run relevant validation themselves and report results, not hand back a list of commands for the human to run.

The repository-wide validation set is:

- `nix develop -c bash -lc 'cd go && go test ./...'`
- `nix build .#cli`
- `nix build .#core`
- `nix flake check`

Docs-only changes may skip builds when genuinely unnecessary, but the report must say that explicitly.

## Reporting contract for agent work

After changes, agents should report in this shape:

1. Verified current behavior
2. What changed
3. Why it changed
4. Validation results
5. Remaining unsupported or unresolved behavior
6. Human decisions still needed

Do not end routine agent reports with "commands to run next" unless a human explicitly asks for them.

## Current repository priorities

The highest-risk domains right now are:

- normalized transaction contract and support boundaries
- Solana asset identity and multi-leg transaction handling
- Hyperliquid funding and perpetual semantics
- EVM swaps, bridges, and own-wallet transfer matching
- asset identity / symbol resolution / USD valuation gaps
- audit surfacing for unresolved cases

These should be worked evidence-first and checkpoint-by-checkpoint.
