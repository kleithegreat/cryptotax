# Repository Review

This document tracks repository-wide gaps, divergences, and follow-up work that are larger than any single domain.

## Open gaps

### 1. Agent-first docs bootstrap still has follow-on cleanup

Status: open

The repository now has an agent-first `AGENTS.md` plus repo-wide `SPEC.md`, `ARCHITECTURE.md`, `REVIEW.md`, and `QUIRKS.md` coverage. The follow-on cleanup is still open because older docs and evidence logs have not yet been fully reorganized around the new hierarchy.

Needed:

- continue filling in `REVIEW.md` and `QUIRKS.md` only where concrete domain tension or operational gotchas justify them
- migrate overlapping content from older monolithic docs into domain docs intentionally
- keep `docs/repo/TASK_DAG.md` current so blocked work, human gates, and cross-domain dependencies stay visible

### 2. Support tiers are not yet documented consistently across domains

Status: open

The repository already distinguishes, in practice, between verified fixtures, current-behavior regression guards, and unresolved cases. That distinction now needs to be made explicit in each domain spec.

Needed:

- mark behaviors as supported, current-behavior-only, unsupported but surfaced, or human-verified exemplar
- keep that labeling consistent between specs, review docs, fixtures, and summaries

### 3. Human verification evidence is still concentrated in one file

Status: open

`docs/known-transactions.md` is useful, but it currently mixes multiple domains and review intents in one place.

Needed:

- gradually split domain-specific evidence and review items into the relevant domain docs
- keep `known-transactions.md` as a compatibility layer until the new structure is mature enough to replace it intentionally

### 4. First domain review docs exist, but review and quirks layering is still incomplete

Status: open

The repository now has grounded `REVIEW.md` coverage for `docs/ir`, `docs/core`, `docs/solana`, and `docs/hyperliquid`. `docs/evm/REVIEW.md` is still intentionally absent because `docs/evm/SPEC.md` and `docs/evm/ARCHITECTURE.md` currently describe the same deliberately narrow conservative boundary rather than a separate spec-versus-implementation divergence.

Needed:

- keep the new domain review docs current as code and support claims change
- add `docs/evm/REVIEW.md` only once a real EVM spec-versus-implementation gap is grounded
- continue filling `QUIRKS.md` only where concrete source-specific gotchas justify it
