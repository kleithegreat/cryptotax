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

Status: open (partial progress)

The repository already distinguishes, in practice, between verified fixtures, current-behavior regression guards, and unresolved cases. That distinction now needs to be made explicit in each domain spec.

Progress:

- `docs/known-transactions.md` now has an explicit support-tier mapping that labels cases with regression fixtures as **current-behavior-only** and cases without as **unsupported but surfaced**, plus the verified golden fixture as **human-verified exemplar**
- domain spec tier headings in `solana`, `hyperliquid`, `evm`, and `core` now use canonical tier names from `docs/repo/SPEC.md`

Remaining:

- mark behaviors in domain architecture docs using the canonical tier labels where headings still use non-canonical wording
- keep that labeling consistent between specs, review docs, fixtures, and summaries

### 3. Human verification evidence is still concentrated in one file

Status: open

`docs/known-transactions.md` is useful, but it currently mixes multiple domains and review intents in one place.

Needed:

- gradually split domain-specific evidence and review items into the relevant domain docs
- keep `known-transactions.md` as a compatibility layer until the new structure is mature enough to replace it intentionally

### 4. First domain review docs exist, but review and quirks layering is still incomplete

Status: open (partial progress)

The repository now has grounded `REVIEW.md` coverage for `docs/ir`, `docs/core`, `docs/solana`, and `docs/hyperliquid`. `docs/evm/REVIEW.md` is still intentionally absent because `docs/evm/SPEC.md` and `docs/evm/ARCHITECTURE.md` currently describe the same deliberately narrow conservative boundary rather than a separate spec-versus-implementation divergence.

Progress:

- `docs/evm/QUIRKS.md` now captures the Etherscan spam-token symbol gotcha and the zero-amount row artifact, both grounded in `docs/known-transactions.md` evidence
- `docs/solana/QUIRKS.md` now captures Helius mint-as-asset-name, float64 token rendering, fee attachment in multi-row parses, and enhanced endpoint fallback, all grounded in `docs/solana/ARCHITECTURE.md` and `docs/known-transactions.md` evidence
- `docs/hyperliquid/QUIRKS.md` now captures all-zero funding hash, market context loss, `@N` spot metadata dependency, and partial fill hash sharing, all grounded in `docs/hyperliquid/ARCHITECTURE.md`, `docs/hyperliquid/REVIEW.md`, and `docs/known-transactions.md` evidence

Remaining:

- keep the new domain review docs current as code and support claims change
- add `docs/evm/REVIEW.md` only once a real EVM spec-versus-implementation gap is grounded
- continue filling domain `QUIRKS.md` for other domains where concrete source-specific gotchas justify it
