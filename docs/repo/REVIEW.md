# Repository Review

This document tracks repository-wide gaps, divergences, and follow-up work that are larger than any single domain.

## Open gaps

### 1. Agent-first workflow is only partially bootstrapped

Status: open

The repository now has the beginnings of a domain-doc hierarchy, but the existing root `AGENTS.md` and older docs have not yet been fully reorganized around it.

Needed:

- replace the root `AGENTS.md` with the new agent-first version
- add `ARCHITECTURE.md`, `REVIEW.md`, and `QUIRKS.md` to the highest-risk domains
- migrate overlapping content from older monolithic docs into domain docs intentionally

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

### 4. Implementation is ahead of formalized contracts

Status: open

The repository has meaningful tests and conservative checkpoints already, but several important long-term contracts still need their own domain specs.

Highest priority:

- normalized IR contract
- Haskell core semantics
- Solana identity and multi-leg handling
- Hyperliquid support boundary
- EVM bridge and swap support boundary
