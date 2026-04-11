# Repository Task DAG

This file is the lightweight cross-domain task DAG for `cryptotax`.

It should track only work or checkpoints that are already grounded in repository docs, fixtures, or explicit human instructions. Do not use it to invent progress.

## How To Update

- add or rename tasks only when a repo doc, fixture, or explicit human instruction already establishes the gap or checkpoint
- prefer one task per concrete dependency boundary instead of vague backlog buckets
- use `done` only for checkpointed repository state that already exists
- if a task needs a human decision or a human verification step, represent that as its own task instead of hiding it inside a generic `blocked` item

## Statuses

- `ready`
- `in_progress`
- `blocked`
- `needs_human_decision`
- `needs_human_verification`
- `done`

## Edge Labels

- `depends_on`
- `blocked_by`
- `unlocks`

## Default Dependency Rules

- `docs/<domain>/REVIEW.md` stays `blocked` until that domain has a meaningful `SPEC.md` and `ARCHITECTURE.md`.
- support-boundary upgrades stay `blocked` while source truth is unresolved; the blocking task should usually be a `needs_human_verification` exemplar task
- core semantic upgrades that reinterpret conservative upstream rows stay `blocked` until the relevant IR or normalization boundary is explicit

## Nodes

| ID | Status | Task | depends_on | blocked_by | unlocks | Grounding |
| --- | --- | --- | --- | --- | --- | --- |
| `docs.repo_baseline` | `done` | Repo-wide agent-first docs baseline exists in `AGENTS.md` plus `docs/repo/{SPEC,ARCHITECTURE,REVIEW,QUIRKS}.md`. | - | - | `docs.agent_first_follow_on_cleanup`, all review-doc tasks | `AGENTS.md`, `docs/repo/SPEC.md`, `docs/repo/ARCHITECTURE.md`, `docs/repo/REVIEW.md`, `docs/repo/QUIRKS.md` |
| `docs.high_risk_domain_specs` | `done` | `SPEC.md` exists for the current highest-risk domains: `audit`, `ir`, `core`, `solana`, `hyperliquid`, and `evm`. | - | - | all high-risk `REVIEW.md` tasks | `docs/audit/SPEC.md`, `docs/ir/SPEC.md`, `docs/core/SPEC.md`, `docs/solana/SPEC.md`, `docs/hyperliquid/SPEC.md`, `docs/evm/SPEC.md` |
| `docs.high_risk_domain_architecture` | `done` | `ARCHITECTURE.md` exists for the current highest-risk domains: `audit`, `ir`, `core`, `solana`, `hyperliquid`, and `evm`. | - | - | all high-risk `REVIEW.md` tasks | `docs/audit/ARCHITECTURE.md`, `docs/ir/ARCHITECTURE.md`, `docs/core/ARCHITECTURE.md`, `docs/solana/ARCHITECTURE.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/evm/ARCHITECTURE.md` |
| `audit.evidence_workflow_baseline` | `done` | Audit capture, filtering, summary, and the current human evidence log already exist. | - | - | `verification.expand_human_verified_exemplars`, `docs.agent_first_follow_on_cleanup`, `docs.review.evm`, `docs.review.solana`, `docs.review.hyperliquid` | `docs/audit-workflow.md`, `docs/known-transactions.md`, `docs/audit/SPEC.md`, `docs/audit/ARCHITECTURE.md` |
| `docs.agent_first_follow_on_cleanup` | `ready` | Continue the docs bootstrap by splitting mixed evidence out of `docs/known-transactions.md`, keeping support-tier labeling consistent, and adding `QUIRKS.md` only where concrete source-specific gotchas justify it. | `docs.repo_baseline`, `audit.evidence_workflow_baseline` | - | cleaner domain docs and future evidence maintenance | `docs/repo/REVIEW.md` gaps 2-4 and `docs/repo/QUIRKS.md` |
| `docs.review.ir` | `done` | `docs/ir/REVIEW.md` now captures the grounded IR gaps around canonical-versus-display identity collapse, multi-row representation ambiguity, and skipped-row surfacing. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.ir_contract_refinement`, clearer downstream core boundaries | `docs/ir/SPEC.md`, `docs/ir/ARCHITECTURE.md`, `docs/ir/REVIEW.md` |
| `docs.review.core` | `done` | `docs/core/REVIEW.md` now captures the grounded core gaps around dropped transfer rows, missing non-8949 output channels, and negative funding expense semantics. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.hyperliquid_negative_funding_output`, `core.accounting_support_upgrade` | `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/core/REVIEW.md` |
| `docs.review.solana` | `done` | `docs/solana/REVIEW.md` now captures the grounded Solana gaps around mint-versus-symbol collapse, unresolved valuation surfacing, and ambiguous multi-row transaction output. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.ir_contract_refinement`, `solana.identity_valuation_support_boundary_upgrade` | `docs/solana/SPEC.md`, `docs/solana/ARCHITECTURE.md`, `docs/solana/REVIEW.md` |
| `docs.review.hyperliquid` | `done` | `docs/hyperliquid/REVIEW.md` now captures the grounded Hyperliquid gaps around funding evidence retention, negative funding output semantics, and perp spot-like approximation boundaries. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.hyperliquid_negative_funding_output`, `decision.hyperliquid_perp_model_boundary`, Hyperliquid support upgrades | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/hyperliquid/REVIEW.md` |
| `docs.review.evm` | `done` | Assessed `docs/evm/REVIEW.md` readiness and kept it intentionally absent because the current EVM spec and architecture already agree on a deliberately narrow conservative boundary. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture`, `audit.evidence_workflow_baseline` | - | `evm.bridge_swap_support_upgrade` | `docs/evm/SPEC.md`, `docs/evm/ARCHITECTURE.md`, `docs/repo/REVIEW.md` |
| `verification.expand_human_verified_exemplars` | `needs_human_verification` | Expand the set of human-verified exemplars from the currently pending real-wallet cases before upgrading support claims. Current priority labels already documented are the Solana, Hyperliquid, and EVM cases in `docs/known-transactions.md`. | `audit.evidence_workflow_baseline` | - | `solana.identity_valuation_support_boundary_upgrade`, `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `hyperliquid.perp_semantics_upgrade`, `evm.bridge_swap_support_upgrade` | `docs/audit/SPEC.md`, `docs/known-transactions.md`, `docs/repo/REVIEW.md` |
| `decision.ir_contract_refinement` | `needs_human_decision` | Decide whether and how the normalized IR should preserve canonical asset identity separately from display identity, and whether any schema change is justified. | `docs.review.ir`, `docs.review.solana` | - | `solana.identity_valuation_support_boundary_upgrade`, `core.accounting_support_upgrade`, future IR implementation work | `docs/ir/SPEC.md`, `docs/ir/ARCHITECTURE.md`, `docs/solana/SPEC.md`, `AGENTS.md` human-decision boundary |
| `decision.hyperliquid_negative_funding_output` | `needs_human_decision` | Decide how ordinary negative Hyperliquid funding expense should appear in final output, if at all, without overstating current support. | `docs.review.core`, `docs.review.hyperliquid` | - | `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `core.accounting_support_upgrade` | `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md` |
| `decision.hyperliquid_perp_model_boundary` | `needs_human_decision` | Decide whether future Hyperliquid support should remain explicitly current-behavior-only spot-like output or adopt a distinct perp position and PnL model. | `docs.review.hyperliquid` | - | `hyperliquid.perp_semantics_upgrade`, `core.accounting_support_upgrade` | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `AGENTS.md` human-decision boundary |
| `solana.identity_valuation_support_boundary_upgrade` | `blocked` | Upgrade Solana support beyond the current conservative boundary for mint identity, display identity, and unresolved valuation handling. | `docs.review.solana` | `verification.expand_human_verified_exemplars`, `decision.ir_contract_refinement` | `core.accounting_support_upgrade` | `docs/solana/SPEC.md`, `docs/solana/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | `blocked` | Upgrade Hyperliquid funding handling beyond today's normalization and unsupported-expense surfacing, including stronger evidence linkage and any supported expense output. | `docs.review.hyperliquid`, `docs.review.core` | `verification.expand_human_verified_exemplars`, `decision.hyperliquid_negative_funding_output` | `core.accounting_support_upgrade` | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `hyperliquid.perp_semantics_upgrade` | `blocked` | Upgrade Hyperliquid perp handling beyond the current spot-like `buy` / `sell` / `swap` approximation. | `docs.review.hyperliquid` | `verification.expand_human_verified_exemplars`, `decision.ir_contract_refinement`, `decision.hyperliquid_perp_model_boundary` | `core.accounting_support_upgrade` | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `evm.bridge_swap_support_upgrade` | `blocked` | Expand EVM bridge or swap support beyond the intentionally narrow confirmed bridge matcher and conservative split-row handling. | `docs.review.evm` | `verification.expand_human_verified_exemplars` | downstream EVM support-claim updates | `docs/evm/SPEC.md`, `docs/evm/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `core.accounting_support_upgrade` | `blocked` | Upgrade core accounting semantics beyond today's conservative downstream handling once upstream IR and normalization boundaries are explicit. | `docs.review.core` | `decision.ir_contract_refinement`, `decision.hyperliquid_negative_funding_output`, `decision.hyperliquid_perp_model_boundary` | stronger supported output claims in the Haskell core | `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/ir/SPEC.md`, `docs/repo/REVIEW.md` |

## Initial Critical Paths

- `decision.ir_contract_refinement` -> `solana.identity_valuation_support_boundary_upgrade` -> `core.accounting_support_upgrade`
- `verification.expand_human_verified_exemplars` -> `decision.hyperliquid_negative_funding_output` -> `hyperliquid.funding_evidence_and_expense_semantics_upgrade` -> `core.accounting_support_upgrade`
- `verification.expand_human_verified_exemplars` -> `decision.hyperliquid_perp_model_boundary` -> `hyperliquid.perp_semantics_upgrade` -> `core.accounting_support_upgrade`
- `verification.expand_human_verified_exemplars` -> `evm.bridge_swap_support_upgrade`

## Ready Now

- `docs.agent_first_follow_on_cleanup`
