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
| `docs.agent_first_follow_on_cleanup` | `in_progress` | Continue the docs bootstrap by splitting mixed evidence out of `docs/known-transactions.md`, keeping support-tier labeling consistent, and adding `QUIRKS.md` only where concrete source-specific gotchas justify it. First checkpoint: fixed absolute paths, added support-tier mapping to `known-transactions.md`, created `docs/evm/QUIRKS.md`. Second checkpoint: created `docs/solana/QUIRKS.md` and `docs/hyperliquid/QUIRKS.md` from evidenced gotchas, normalized domain spec tier headings to canonical labels, updated repo-level review and quirks docs. | `docs.repo_baseline`, `audit.evidence_workflow_baseline` | - | cleaner domain docs and future evidence maintenance | `docs/repo/REVIEW.md`, `docs/repo/QUIRKS.md`, `docs/evm/QUIRKS.md`, `docs/solana/QUIRKS.md`, `docs/hyperliquid/QUIRKS.md` |
| `docs.review.ir` | `done` | `docs/ir/REVIEW.md` now captures the grounded IR work around the resolved `asset_canonical` contract, resolved `event_group_id` / `split_reason` metadata, and skipped-row surfacing. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.ir_contract_refinement`, clearer downstream core boundaries | `docs/ir/SPEC.md`, `docs/ir/ARCHITECTURE.md`, `docs/ir/REVIEW.md` |
| `docs.review.core` | `done` | `docs/core/REVIEW.md` now captures the grounded core gaps around dropped transfer rows plus the resolved income-report, negative-funding, and separate-perp-report boundaries. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.hyperliquid_negative_funding_output`, `core.accounting_support_upgrade` | `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/core/REVIEW.md` |
| `docs.review.solana` | `done` | `docs/solana/REVIEW.md` now captures the resolved canonical-identity checkpoint plus the remaining Solana gaps around unresolved valuation surfacing and ambiguous multi-row transaction output. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.ir_contract_refinement`, `solana.identity_valuation_support_boundary_upgrade` | `docs/solana/SPEC.md`, `docs/solana/ARCHITECTURE.md`, `docs/solana/REVIEW.md` |
| `docs.review.hyperliquid` | `done` | `docs/hyperliquid/REVIEW.md` now captures the remaining Hyperliquid gaps around funding evidence strength and partial-fill consolidation, plus the resolved negative-funding and separate-perp-report boundaries. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture` | - | `decision.hyperliquid_negative_funding_output`, `decision.hyperliquid_perp_model_boundary`, Hyperliquid support upgrades | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/hyperliquid/REVIEW.md` |
| `docs.review.evm` | `done` | Assessed `docs/evm/REVIEW.md` readiness and kept it intentionally absent because the current EVM spec and architecture already agree on a deliberately narrow conservative boundary. | `docs.high_risk_domain_specs`, `docs.high_risk_domain_architecture`, `audit.evidence_workflow_baseline` | - | `evm.bridge_swap_support_upgrade` | `docs/evm/SPEC.md`, `docs/evm/ARCHITECTURE.md`, `docs/repo/REVIEW.md` |
| `verification.hyperliquid.packet_prep` | `done` | Hyperliquid verification packet prepared with four pending exemplar cases grouped by blocker category, exact unresolved questions, and DAG unlock mapping. | `audit.evidence_workflow_baseline` | - | `verification.expand_human_verified_exemplars` | `docs/hyperliquid/verification-packet.md`, `docs/known-transactions.md` labels `hl-funding-negative-2025-10-07`, `hl-funding-positive-2025-12-02`, `hl-open-long-btc`, `hl-close-short-sol` |
| `verification.solana.packet_prep` | `done` | Solana verification packet prepared with three pending exemplar cases (`sol-pumpfun-zero-usd`, `sol-dflow-swap`, `sol-addresslike-mint`) grouped by blocker category (identity, valuation, economic interpretation), exact unresolved questions, and DAG unlock mapping. | `audit.evidence_workflow_baseline`, `docs.review.solana` | - | `verification.expand_human_verified_exemplars` | `docs/solana/verification-packet.md`, `docs/known-transactions.md` labels `sol-pumpfun-zero-usd`, `sol-dflow-swap`, `sol-addresslike-mint` |
| `verification.evm.packet_prep` | `done` | EVM verification packet prepared with seven pending exemplar cases grouped by blocker category (bridge pairing, swap semantic interpretation, own-wallet confirmation, suspicious-asset identity), exact unresolved questions, and support-boundary unlock mapping. | `audit.evidence_workflow_baseline`, `docs.review.evm` | - | `verification.expand_human_verified_exemplars` | `docs/evm/verification-packet.md`, `docs/known-transactions.md` labels `eth-own-transfer-out`, `eth-airdrop-epin`, `eth-swap-usdc`, `eth-bridge-out-usdc`, `arb-bridge-in-fillrelay`, `arb-claimarb-label`, `arb-claim-pool-label` |
| `verification.expand_human_verified_exemplars` | `needs_human_verification` | Expand the set of human-verified exemplars from the currently pending real-wallet cases before upgrading support claims. Current priority labels already documented are the Solana, Hyperliquid, and EVM cases in `docs/known-transactions.md`. Hyperliquid packet in `docs/hyperliquid/verification-packet.md`; Solana packet in `docs/solana/verification-packet.md`; EVM packet in `docs/evm/verification-packet.md`. | `audit.evidence_workflow_baseline`, `verification.hyperliquid.packet_prep`, `verification.solana.packet_prep`, `verification.evm.packet_prep` | - | `solana.identity_valuation_support_boundary_upgrade`, `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `hyperliquid.perp_semantics_upgrade`, `evm.bridge_swap_support_upgrade` | `docs/audit/SPEC.md`, `docs/known-transactions.md`, `docs/repo/REVIEW.md`, `docs/hyperliquid/verification-packet.md`, `docs/solana/verification-packet.md`, `docs/evm/verification-packet.md` |
| `decision.ir_contract_refinement` | `done` | Option B chosen: add optional `asset_canonical` to `AssetAmount`. Implemented in Go and Haskell types. Helius normalizer populates it when mint differs from display symbol. Backward compatible. | `docs.review.ir`, `docs.review.solana` | - | `solana.identity_valuation_support_boundary_upgrade`, `core.accounting_support_upgrade`, future IR implementation work | `docs/repo/decision-ir-contract-refinement.md`, `go/types/types.go`, `haskell/src/Types.hs`, `go/normalize/normalize.go` |
| `decision.hyperliquid_negative_funding_output` | `done` | Option C chosen: emit negative funding in a structured `funding_expenses.csv` report, separate from 8949 and perp PnL. Implemented as `FundingExpense` entries in `ProcessResult`. No USDC lot consumption. | `docs.review.core`, `docs.review.hyperliquid` | - | `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `core.accounting_support_upgrade` | `docs/repo/decision-hyperliquid-negative-funding-output.md`, `haskell/src/GainLoss.hs`, `haskell/src/Report.hs`, `haskell/app/Main.hs` |
| `decision.hyperliquid_perp_model_boundary` | `done` | Option C chosen: quarantine perps from spot FIFO with `perp_open`/`perp_close` types, propagate `ClosedPnl`, emit realized PnL in `perp_pnl.csv`. Implemented end-to-end from fetcher through core output. | `docs.review.hyperliquid` | - | `hyperliquid.perp_semantics_upgrade`, `core.accounting_support_upgrade` | `docs/repo/decision-hyperliquid-perp-model-boundary.md`, `go/fetcher/hyperliquid.go`, `go/normalize/normalize.go`, `haskell/src/GainLoss.hs` |
| `solana.identity_valuation_support_boundary_upgrade` | `blocked` | Upgrade Solana support beyond the current conservative boundary for mint identity, display identity, and unresolved valuation handling. IR schema now supports `asset_canonical`. | `docs.review.solana` | `verification.expand_human_verified_exemplars` | downstream Solana support-claim updates | `docs/solana/SPEC.md`, `docs/solana/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | `blocked` | Upgrade Hyperliquid funding handling: stronger evidence linkage on top of preserved `market` and `event_group_id`, plus any future lot-consumption policy. Core now has structured expense output. | `docs.review.hyperliquid`, `docs.review.core` | `verification.expand_human_verified_exemplars` | downstream Hyperliquid support-claim updates | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `hyperliquid.perp_semantics_upgrade` | `blocked` | Further perp improvements: partial-fill consolidation and any future position-tracking refinements. Canonical output keeps perps in separate `perp_pnl.csv` reporting rather than 8949. | `docs.review.hyperliquid` | `verification.expand_human_verified_exemplars` | downstream Hyperliquid support-claim updates | `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `evm.bridge_swap_support_upgrade` | `blocked` | Expand EVM bridge or swap support beyond the intentionally narrow confirmed bridge matcher and conservative split-row handling. | `docs.review.evm` | `verification.expand_human_verified_exemplars` | downstream EVM support-claim updates | `docs/evm/SPEC.md`, `docs/evm/ARCHITECTURE.md`, `docs/known-transactions.md` |
| `core.accounting_support_upgrade` | `done` | Implementation checkpoint completed: IR carries additive `asset_canonical`, `market`, `event_group_id`, and `split_reason` metadata; the core writes `income.csv`, `funding_expenses.csv`, and separate `perp_pnl.csv`; perps remain quarantined from spot FIFO. Canonical-aware downstream adoption and evidence-backed support claims remain separate tasks. Validation currently passes with `go test ./...` and `nix flake check`. | `docs.review.core` | - | downstream support-boundary cleanup once evidence is verified | `docs/core/SPEC.md`, `docs/core/ARCHITECTURE.md`, `docs/ir/SPEC.md`, `go/types/types.go`, `haskell/src/Types.hs`, `haskell/src/GainLoss.hs`, `haskell/src/Report.hs`, `haskell/test/Spec.hs` |
| `ir.skipped_row_structured_diagnostics` | `done` | `NormalizeWithDiagnostics` returns structured `[]SkippedRow` alongside normalized transactions. `Normalize` wraps it with stderr logging for backward compatibility. | `docs.review.ir` | - | `ir.skipped_row_audit_persistence` | `go/normalize/normalize.go`, `go/normalize/normalize_test.go`, `docs/ir/ARCHITECTURE.md`, `docs/ir/REVIEW.md` |
| `ir.skipped_row_audit_persistence` | `done` | `buildPayload` calls `NormalizeWithDiagnostics` and returns `[]SkippedRow`. `WriteCaptureArtifacts` persists skipped rows as a `.skipped.json` sidecar. `audit capture` threads the data through and reports the sidecar path. | `ir.skipped_row_structured_diagnostics` | - | cleaner audit evidence for payload completeness | `docs/ir/REVIEW.md`, `go/cmd/main.go`, `go/audit/audit.go`, `go/audit/audit_test.go` |

## Critical Paths (updated)

All three implementation decisions are resolved and wired through the code. Remaining domain-specific support-boundary upgrades are all blocked by `verification.expand_human_verified_exemplars`:

- `verification.expand_human_verified_exemplars` -> `solana.identity_valuation_support_boundary_upgrade`
- `verification.expand_human_verified_exemplars` -> `hyperliquid.funding_evidence_and_expense_semantics_upgrade`
- `verification.expand_human_verified_exemplars` -> `hyperliquid.perp_semantics_upgrade`
- `verification.expand_human_verified_exemplars` -> `evm.bridge_swap_support_upgrade`

## Blocked on Human Verification

- `solana.identity_valuation_support_boundary_upgrade`
- `hyperliquid.funding_evidence_and_expense_semantics_upgrade`
- `hyperliquid.perp_semantics_upgrade`
- `evm.bridge_swap_support_upgrade`

## In Progress

- `docs.agent_first_follow_on_cleanup`
