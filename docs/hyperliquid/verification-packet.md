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
| perp interpretation | Perpetual activity is approximated as spot-like buy/sell and routed through spot lot accounting. |
| multiple | More than one of the above applies. |

---

## Case 1: `hl-funding-negative-2025-10-07`

- **Label:** `hl-funding-negative-2025-10-07`
- **Source system:** Hyperliquid funding history
- **Known-transactions entry:** `hl-funding-negative-2025-10-07`
- **Current normalized description:** 1 row; `funding_payment` sent USDC 0.168095 @ 0.168095 USD
- **Regression fixture:** `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json`
- **Filtered case file:** `audit/cases/hl-funding-negative-2025-10-07.json`

### Unresolved question for the human

1. Retain and review the Hyperliquid source row showing `delta.usdc -0.168095` for `2025-10-07T00:00:00Z`. Does the source data confirm the amount, timestamp, and market?
2. The normalized row loses which Hyperliquid market produced this funding payment (see the divergence note in `docs/hyperliquid/ARCHITECTURE.md` and the `Funding rows lose market context and still have weak evidence linkage` review item in `docs/hyperliquid/REVIEW.md`). Is the current USDC-only representation acceptable for evidence purposes, or must market context be retained before this case can be verified?
3. Decide how this ordinary negative funding expense should appear in final tax output (see the `Negative funding is preserved, but final output semantics are still undecided` review item in `docs/hyperliquid/REVIEW.md` and the `Negative funding_payment is preserved but has no decided accounting or output semantics` review item in `docs/core/REVIEW.md`).

### Blocker category

**Multiple** — evidence linkage (market context dropped, source row not yet retained) and final output semantics (no decided expense treatment).

### Support-boundary upgrade this case could unlock

Resolving evidence linkage contributes to unblocking `hyperliquid.funding_evidence_and_expense_semantics_upgrade` in `docs/repo/TASK_DAG.md`. Resolving final output semantics additionally contributes to unblocking `decision.hyperliquid_negative_funding_output`, which chains into `core.accounting_support_upgrade`.

---

## Case 2: `hl-funding-positive-2025-12-02`

- **Label:** `hl-funding-positive-2025-12-02`
- **Source system:** Hyperliquid funding history
- **Known-transactions entry:** `hl-funding-positive-2025-12-02`
- **Current normalized description:** 1 row; `funding_payment` received USDC 1.879512 @ 1.879512 USD
- **Regression fixture:** `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json`
- **Filtered case file:** `audit/cases/hl-funding-positive-2025-12-02.json`

### Unresolved question for the human

1. Retain and review the Hyperliquid source row showing `delta.usdc 1.879512` for `2025-12-02T00:00:00Z`. Does the source data confirm the amount, timestamp, and market?
2. The current normalized `id` is the all-zero hash propagated from the Hyperliquid API. Is this acceptable evidence linkage, or do funding rows need a richer synthetic identifier before this case can be verified? (See the `Funding rows lose market context and still have weak evidence linkage` review item in `docs/hyperliquid/REVIEW.md` and the divergence note in `docs/hyperliquid/ARCHITECTURE.md`.)
3. The normalized row drops market context. Should the reviewer require market context to be preserved in the IR before signing off, or is USDC-flow-only verification sufficient for the positive funding path?

### Blocker category

**Evidence linkage** — the all-zero source hash and dropped market context make it difficult to trace this row back to the specific Hyperliquid funding event.

### Support-boundary upgrade this case could unlock

Resolving evidence linkage for positive funding contributes to unblocking `hyperliquid.funding_evidence_and_expense_semantics_upgrade`. Positive funding already reaches the Haskell income path, so verified evidence linkage here would strengthen the existing income-receipt support claim without requiring an output-semantics decision.

---

## Case 3: `hl-open-long-btc`

- **Label:** `hl-open-long-btc`
- **Source system:** Hyperliquid fill history
- **Known-transactions entry:** `hl-open-long-btc`
- **Current normalized description:** 1 row; `buy` BTC 0.0004 @ 49.92600000 USD with USDC fee 0.022466
- **Filtered case file:** `audit/cases/hl-open-long-btc.json`

### Unresolved question for the human

1. Confirm fill `0x296c458688c54a1e2ae6042cfc24eb02026d006c23c868f0cd34f0d947c92408` from Hyperliquid. Was this an opening perpetual position increase, or could it have been a spot acquisition?
2. The current normalization maps `OPEN LONG` to a spot-like `buy` row that enters FIFO lot accounting (see `docs/hyperliquid/ARCHITECTURE.md` and the `Perp fills cross the IR as spot-like rows at API-fill granularity` review item in `docs/hyperliquid/REVIEW.md`). Is the current spot-like approximation acceptable as an explicitly current-behavior-only checkpoint, or should it be flagged as semantically misleading before any downstream tax claim touches it?
3. If a dedicated perp model is adopted in the future (see `decision.hyperliquid_perp_model_boundary` in `docs/repo/TASK_DAG.md`), how should this row be reinterpreted — as a position-size increase with no immediate tax event, or as something else?

### Blocker category

**Perp interpretation** — this perpetual position opening is currently approximated as a spot `buy` and routed through spot inventory semantics.

### Support-boundary upgrade this case could unlock

Verifying the source event and confirming the current approximation boundary contributes to unblocking `hyperliquid.perp_semantics_upgrade`, which is also blocked by `decision.hyperliquid_perp_model_boundary`. Together these chain into `core.accounting_support_upgrade`.

---

## Case 4: `hl-close-short-sol`

- **Label:** `hl-close-short-sol`
- **Source system:** Hyperliquid fill history
- **Known-transactions entry:** `hl-close-short-sol`
- **Current normalized description:** 4 rows; `sell` SOL totals 31.34 units across four partial fills with per-row USDC fees
- **Filtered case file:** `audit/cases/hl-close-short-sol.json`

### Unresolved question for the human

1. Confirm fill `0xd83f79b33a4eb3a3d9b9042d4d89e20204320098d541d2757c082505f9428d8e` from Hyperliquid. Do all four rows belong to one economic short-close event?
2. Confirm fee conservation: do the per-row USDC fees sum to the total fee for this close event as shown in the Hyperliquid source data?
3. The current normalization maps `CLOSE SHORT` to spot-like `sell` rows at API-fill granularity with no later consolidation step (see the known-approximations section in `docs/hyperliquid/ARCHITECTURE.md` and the `Perp fills cross the IR as spot-like rows at API-fill granularity` review item in `docs/hyperliquid/REVIEW.md`). Is the four-row spot-sell approximation acceptable as a current-behavior-only checkpoint, or is the row explosion itself a verification blocker?
4. If a dedicated perp model is adopted, should this become a single position-close event with realized PnL, or should the partial-fill granularity be preserved?

### Blocker category

**Multiple** — perp interpretation (spot-like `sell` approximation for a short close) and evidence linkage (four partial fills that may represent one economic event, with no consolidation or grouping).

### Support-boundary upgrade this case could unlock

Verifying the source event and confirming the current approximation boundary contributes to unblocking `hyperliquid.perp_semantics_upgrade`. The fill-grouping question also feeds into the broader IR contract discussion at `decision.ir_contract_refinement`. Together these chain into `core.accounting_support_upgrade`.

---

## Cross-case summary

| Case | Blocker category | Primary DAG node unblocked | Needs human decision node? |
| --- | --- | --- | --- |
| `hl-funding-negative-2025-10-07` | multiple (evidence linkage + final output semantics) | `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | Yes: `decision.hyperliquid_negative_funding_output` |
| `hl-funding-positive-2025-12-02` | evidence linkage | `hyperliquid.funding_evidence_and_expense_semantics_upgrade` | No (income path already exists) |
| `hl-open-long-btc` | perp interpretation | `hyperliquid.perp_semantics_upgrade` | Yes: `decision.hyperliquid_perp_model_boundary` |
| `hl-close-short-sol` | multiple (perp interpretation + evidence linkage) | `hyperliquid.perp_semantics_upgrade` | Yes: `decision.hyperliquid_perp_model_boundary` |

## Shared open questions across all cases

1. **Market context retention:** All four cases share the gap that `normalizeHyperliquid` drops market context. The funding cases lose `Delta.Coin`; the fill cases preserve `Coin` in the asset field but lose any explicit market/contract identifier. This is documented in `docs/hyperliquid/ARCHITECTURE.md` and the `Funding rows lose market context and still have weak evidence linkage` review item in `docs/hyperliquid/REVIEW.md`.

2. **Source identifier weakness:** The funding cases use an all-zero hash as their `id`. The fill cases use the Hyperliquid fill hash, which is stronger but still one-hash-per-API-row rather than one-hash-per-economic-event. There is no current consolidation step.

3. **IR contract boundary:** Both the perp-fill approximation and the funding-market-context gap ultimately depend on `types.Transaction` having enough fields to carry the necessary context. This is tracked at `decision.ir_contract_refinement` in the DAG.

## Grounding references

All content above is derived from:

- `docs/known-transactions.md` entries `hl-funding-negative-2025-10-07`, `hl-funding-positive-2025-12-02`, `hl-open-long-btc`, and `hl-close-short-sol`
- `docs/hyperliquid/SPEC.md`
- `docs/hyperliquid/ARCHITECTURE.md`
- `docs/hyperliquid/REVIEW.md`
- the `Negative funding_payment is preserved but has no decided accounting or output semantics` review item in `docs/core/REVIEW.md`
- `docs/repo/TASK_DAG.md` nodes: `verification.expand_human_verified_exemplars`, `decision.hyperliquid_negative_funding_output`, `decision.hyperliquid_perp_model_boundary`, `hyperliquid.funding_evidence_and_expense_semantics_upgrade`, `hyperliquid.perp_semantics_upgrade`, `core.accounting_support_upgrade`
- `docs/audit/SPEC.md` human-verification boundary
- `docs/audit-workflow.md`
