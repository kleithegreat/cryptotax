# Hyperliquid Verification Packet

This packet groups the pending Hyperliquid exemplar cases from `docs/known-transactions.md` for human verification. It makes no new semantic claims. Everything here is grounded in existing repo docs.

## How to use this packet

For each case below, the human reviewer should:

1. Confirm or reject the source-row evidence described in the unresolved question.
2. Record the answer back in `docs/known-transactions.md` by replacing the relevant `TODO` line.
3. Once all cases in a blocker category are resolved, the corresponding DAG task can move forward.

## Blocker categories

| Category | Meaning |
| --- | --- |
| evidence linkage | The normalized row cannot be reliably traced back to the Hyperliquid source event that produced it. |
| final output semantics | The system preserves the event but has no decided tax-output representation. |
| perp interpretation | Perpetual activity now uses dedicated `perp_open` / `perp_close` rows, but the current boundary still needs human verification before support claims rely on it. |
| multiple | More than one of the above applies. |

---

## Case 1: `hl-funding-negative-2025-10-07`

- **Label:** `hl-funding-negative-2025-10-07`
- **Source system:** Hyperliquid funding history
- **Known-transactions entry:** `hl-funding-negative-2025-10-07`
- **Current normalized description:** The checked-in filtered case file predates the market/event-group upgrade. Current implementation should emit 1 row; `funding_payment` sent USDC 0.168095 @ 0.168095 USD plus preserved `market` and `event_group_id`.
- **Regression fixture:** `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json`
- **Filtered case file:** `audit/cases/hl-funding-negative-2025-10-07.json`

### Unresolved question for the human

1. Retain and review the Hyperliquid source row showing `delta.usdc -0.168095` for `2025-10-07T00:00:00Z`. Does the source data confirm the amount, timestamp, and market?
2. The normalized row now preserves `market` and `event_group_id` in addition to the USDC flow. Is that enough evidence linkage for this case, or is the all-zero Hyperliquid `id` still too weak to treat the row as verified?
3. The repo emits this ordinary negative funding expense in a separate `funding_expenses.csv` report without USDC lot consumption. Once the evidence is verified, is that current reporting boundary acceptable?

### Blocker category

**Evidence linkage** — market context is now preserved, but human verification still has to decide whether the all-zero source `id` plus synthetic `event_group_id` is strong enough evidence.

### Support-boundary upgrade this case could unlock

Resolving evidence linkage and confirming the current separate-report boundary contributes to unblocking `hyperliquid.funding_evidence_and_expense_semantics_upgrade`. The negative-funding output checkpoint itself is already implemented; the remaining work is evidence-backed support-boundary cleanup.

---

## Case 2: `hl-funding-positive-2025-12-02`

- **Label:** `hl-funding-positive-2025-12-02`
- **Source system:** Hyperliquid funding history
- **Known-transactions entry:** `hl-funding-positive-2025-12-02`
- **Current normalized description:** The checked-in filtered case file predates the market/event-group upgrade. Current implementation should emit 1 row; `funding_payment` received USDC 1.879512 @ 1.879512 USD plus preserved `market` and `event_group_id`.
- **Regression fixture:** `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json`
- **Filtered case file:** `audit/cases/hl-funding-positive-2025-12-02.json`

### Unresolved question for the human

1. Retain and review the Hyperliquid source row showing `delta.usdc 1.879512` for `2025-12-02T00:00:00Z`. Does the source data confirm the amount, timestamp, and market?
2. The current normalized `id` is still the all-zero hash propagated from the Hyperliquid API, but the row now also carries preserved `market` and `event_group_id`. Is that enough evidence linkage, or is a stronger source identifier still required before this case can be verified?

### Blocker category

**Evidence linkage** — the all-zero source hash still makes this row weaker than a source-backed unique identifier even though `market` and `event_group_id` are now preserved.

### Support-boundary upgrade this case could unlock

Resolving evidence linkage for positive funding contributes to unblocking `hyperliquid.funding_evidence_and_expense_semantics_upgrade`. Positive funding already reaches the Haskell income path, so verified evidence linkage here would strengthen the existing income-receipt support claim without reopening the already-resolved negative-funding output decision.

---

## Case 3: `hl-open-long-btc`

- **Label:** `hl-open-long-btc`
- **Source system:** Hyperliquid fill history
- **Known-transactions entry:** `hl-open-long-btc`
- **Current normalized description:** The checked-in filtered case file is stale and still shows one spot-like `buy` row. Current implementation should emit 1 row; `perp_open` BTC 0.0004 @ 49.92600000 USD with USDC fee 0.022466.
- **Filtered case file:** `audit/cases/hl-open-long-btc.json`

### Unresolved question for the human

1. Confirm fill `0x296c458688c54a1e2ae6042cfc24eb02026d006c23c868f0cd34f0d947c92408` from Hyperliquid. Was this an opening perpetual position increase, or could it have been a spot acquisition?
2. The current normalization maps `OPEN LONG` to `perp_open`, quarantined from spot FIFO with no lot creation (see `docs/hyperliquid/ARCHITECTURE.md`). Is that no-lot boundary acceptable until a fuller position model exists?
3. If a fuller perp model is adopted later, should this remain a pure position-size increase with no immediate tax event, or should additional evidence/context be carried downstream?

### Blocker category

**Perp interpretation** — this perpetual position opening is no longer routed through spot inventory semantics, but the no-lot boundary still needs human verification.

### Support-boundary upgrade this case could unlock

Verifying the source event and confirming the current no-lot boundary contributes to unblocking `hyperliquid.perp_semantics_upgrade`. The perp-quarantine implementation checkpoint is already done; the remaining work is evidence-backed support-boundary cleanup.

---

## Case 4: `hl-close-short-sol`

- **Label:** `hl-close-short-sol`
- **Source system:** Hyperliquid fill history
- **Known-transactions entry:** `hl-close-short-sol`
- **Current normalized description:** The checked-in filtered case file is stale and still shows four spot-like `sell` rows without `closed_pnl`. Current implementation should emit 4 rows; `perp_close` SOL totals 31.34 units across four partial fills with per-row USDC fees plus exchange `closed_pnl` on each row.
- **Filtered case file:** `audit/cases/hl-close-short-sol.json`

### Unresolved question for the human

1. Confirm fill `0xd83f79b33a4eb3a3d9b9042d4d89e20204320098d541d2757c082505f9428d8e` from Hyperliquid. Do all four rows belong to one economic short-close event?
2. Confirm fee conservation: do the per-row USDC fees sum to the total fee for this close event as shown in the Hyperliquid source data?
3. The current normalization maps `CLOSE SHORT` to `perp_close` rows at API-fill granularity, using exchange `closed_pnl` in a separate `perp_pnl.csv` report with no later consolidation step (see `docs/hyperliquid/ARCHITECTURE.md` and `docs/hyperliquid/REVIEW.md`). Is the four-row partial-fill output acceptable as a current-behavior-only checkpoint, or is the row explosion itself a verification blocker?
4. Should this remain four separate per-fill realized-PnL entries, or should partial fills be consolidated before later reporting?

### Blocker category

**Multiple** — perp interpretation (per-fill `perp_close` rows using exchange `closed_pnl`) and evidence linkage (four partial fills that may represent one economic event, with no consolidation or grouping).

### Support-boundary upgrade this case could unlock

Verifying the source event and confirming the current partial-fill boundary contributes to unblocking `hyperliquid.perp_semantics_upgrade`. The fill-grouping question also feeds into the open IR review item about intentional multi-row preservation versus current-behavior splitting in `docs/ir/REVIEW.md`.

---

## Cross-case summary

| Case | Blocker category | Primary DAG node unblocked | Needs human decision node? |
| --- | --- | --- | --- |
| `hl-funding-negative-2025-10-07` | evidence linkage | `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | No: output decision resolved; human verification still pending |
| `hl-funding-positive-2025-12-02` | evidence linkage | `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | No (income path already exists) |
| `hl-open-long-btc` | perp interpretation | `hyperliquid.perp_semantics_upgrade` | No: decision resolved; human verification still pending |
| `hl-close-short-sol` | multiple (perp interpretation + evidence linkage) | `hyperliquid.perp_semantics_upgrade` | No: decision resolved; human verification still pending |

## Shared open questions across all cases

1. **Funding evidence strength:** Funding rows now preserve `market` and `event_group_id`, but the Hyperliquid-provided `id` can still be the all-zero hash. Human verification still has to decide whether that upgraded context is sufficient.

2. **Source identifier weakness on fills:** The fill cases use the Hyperliquid fill hash, which is stronger than funding ids but still one-hash-per-API-row rather than one-hash-per-economic-event. There is no current consolidation step.

3. **IR contract boundary:** `event_group_id` and `split_reason` are now present in the IR. The remaining question is not whether the IR can carry grouping metadata, but how downstream reporting should use it for consolidation and support-boundary work.

## Grounding references

All content above is derived from:

- `docs/known-transactions.md` entries `hl-funding-negative-2025-10-07`, `hl-funding-positive-2025-12-02`, `hl-open-long-btc`, and `hl-close-short-sol`
- `docs/hyperliquid/SPEC.md`
- `docs/hyperliquid/ARCHITECTURE.md`
- `docs/hyperliquid/REVIEW.md`
- the structured funding and perp output boundaries in `docs/core/REVIEW.md`
- `docs/repo/TASK_DAG.md` nodes: `verification.expand_human_verified_exemplars`, `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `hyperliquid.perp_semantics_upgrade`
- `docs/audit/SPEC.md` human-verification boundary
- `docs/audit-workflow.md`
