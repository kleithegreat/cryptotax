# Decision: Hyperliquid Negative Funding Output

Status: `needs_human_decision`

Task DAG node: `decision.hyperliquid_negative_funding_output`

## Current behavior

This is the end-to-end path a negative Hyperliquid funding event takes today.

### 1. Fetch (`go/fetcher/hyperliquid.go`)

`fetchFunding` calls the Hyperliquid `userFunding` API. Each funding entry becomes a `RawTransaction` with:

- `ID = hlFunding.Hash` (often the all-zero hash)
- `Asset = hlFunding.Delta.Coin` (the market that generated the funding)
- `Amount = hlFunding.Delta.USDC` (negative for funding paid)
- `RawType = "funding"`

### 2. Normalize (`go/normalize/normalize.go`)

`normalizeHyperliquid` checks `isNegativeDecimal(raw.Amount)` and emits:

- `tx_type = funding_payment`
- `sent = {asset: "USDC", amount: abs(amount), usd_value: abs(amount)}`
- `raw_type = "funding"` (preserved from fetch)

The market context (`Delta.Coin`) is dropped here. Only the USDC flow survives normalization.

### 3. Core accounting (`haskell/src/GainLoss.hs`)

`handleFunding` matches `(Just snt, Nothing)` and appends a text string to `prErrors`:

```
Unsupported negative funding_payment expense for tx <id>: sent <amount> <asset>;
ordinary expense output and inventory adjustment are not implemented yet
```

No lot is created. No lot is consumed. No `GainLoss` entry is produced. No `prIncome` entry is produced.

### 4. Output (`haskell/app/Main.hs`)

- The error string is printed to stderr as `Warning: "Unsupported negative funding_payment expense for tx ..."`.
- `render8949CSV` writes only `prGainLosses` to the output file.
- Negative funding does not appear in the CSV.
- Negative funding does not affect the income summary total printed to stderr.

### 5. Test coverage (`haskell/test/Spec.hs`)

`prop_negativeFundingIsExplicitlyUnsupported` freezes the current behavior: negative funding produces exactly one `prErrors` entry and zero `prGainLosses` entries.

## Current support boundary

### Preserved today

- The USDC amount and direction (outbound sent leg) survive normalization intact.
- The `funding_payment` tx_type is preserved end-to-end.
- The `raw_type = "funding"` is preserved in the normalized IR.
- The core explicitly flags the row as unsupported in `prErrors`.
- The warning is emitted to stderr so a human operator sees it.
- The normalization is frozen by `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json`.
- The core behavior is frozen by `haskell/test/Spec.hs`.

### Lost today

- **Market context**: `hlFunding.Delta.Coin` (which Hyperliquid market produced the funding) is dropped during normalization. The normalized row says only "sent X USDC", not "sent X USDC for funding on BTC market". This is documented in `docs/hyperliquid/REVIEW.md` and `docs/hyperliquid/ARCHITECTURE.md`.
- **Evidence linkage**: the source hash is often all-zeros, so there is no reliable way to link one normalized funding row back to a specific Hyperliquid source event. This is documented in the `hl-funding-negative-2025-10-07` entry in `docs/known-transactions.md`.
- **Inventory effect**: negative funding does not consume USDC lots. If a user paid 0.17 USDC in funding, their USDC lot balance is unaffected.
- **Structured output**: the expense does not appear in any structured file output. It exists only as a stderr warning string.
- **Expense total**: there is no accumulated or summarized negative-funding total. The income total is printed to stderr, but there is no expense counterpart.

## Why final-output treatment is unresolved

Three things must be decided by a human, and they are independent of each other:

1. **Tax semantics**: Is negative Hyperliquid funding an ordinary deductible expense? A reduction in USDC inventory? Both? The answer depends on the user's tax situation and possibly on professional tax advice. The repo cannot safely guess.

2. **Output channel**: The core currently has exactly one structured output: 8949-oriented CSV rendered by `render8949CSV`. Negative funding is not an 8949 disposal. There is no second output channel for income, expenses, or unsupported-but-surfaced rows. This is documented in the `The core has no stable output channel for modeled income or unsupported rows` review item in `docs/core/REVIEW.md`.

3. **Evidence verification**: The real-wallet negative funding exemplar `hl-funding-negative-2025-10-07` in `docs/known-transactions.md` is still pending human review. Support-boundary upgrades should not precede evidence verification.

These three blockers are layered: even if the tax semantics were decided, the output channel does not yet exist. Even if both were decided, the exemplars should be verified first. But a decision on output policy can be made now and implemented later.

## Output-policy options

### Option A: Keep current behavior (unsupported-but-surfaced stderr warning)

- **Accounting truth**: Negative funding is an economic outflow. Ignoring it in output understates expenses.
- **Presentation**: The warning on stderr is visible to an operator but not captured in any structured report.
- **What this requires in code**: Nothing. This is the status quo.
- **What this requires in docs**: Update the support tier to explicitly document that negative funding is intentionally unsupported in output, not accidentally missing. Currently the docs already say this but the framing could be made crisper.
- **Risk**: A user who relies only on the CSV and the income total printed to stderr will miss that funding expenses occurred. The warning text is easy to overlook.

### Option B: Accumulate and summarize negative funding total to stderr

- **Accounting truth**: Same as Option A for structured output. The expense total becomes visible as a summary line alongside the existing income total.
- **Presentation**: Stderr gains a line like `Total funding expenses: $X.XX` next to the existing `Total income: $X.XX`.
- **What this requires in code**: `handleFunding` (or a post-processing step) accumulates negative funding USD into a new `ProcessResult` field or sums `prErrors`-derived values. `Main.main` prints the total.
- **What this requires in docs**: Document that the summary is informational, not a tax claim. Update the support tier to `current-behavior-only` for the summary.
- **Risk**: Low. This is additive, does not change any existing output, and does not claim tax semantics. A user can cross-reference the total against their Hyperliquid records.

### Option C: Emit negative funding rows in a second structured report

- **Accounting truth**: The expense becomes a durable artifact, not just a runtime warning.
- **Presentation**: A separate file (e.g., `expenses.csv` or a multi-section output) includes negative funding rows with timestamp, amount, and source.
- **What this requires in code**: A new output channel in `ProcessResult` and `Main.main`. A renderer for the expense report. This overlaps with the broader `The core has no stable output channel for modeled income or unsupported rows` review item in `docs/core/REVIEW.md`.
- **What this requires in docs**: Define the output schema. Decide support tier for the new report. Update `docs/core/SPEC.md` and `docs/core/ARCHITECTURE.md`.
- **Risk**: This is a larger change that should probably be decided together with the broader non-8949 output question, not just for negative funding alone.

### Option D: Deduct negative funding from USDC inventory (lot consumption)

- **Accounting truth**: USDC lots are reduced by the funding amount, reflecting the economic outflow.
- **Presentation**: The 8949 CSV gains disposal rows for USDC (cost basis equals proceeds when USDC is treated as 1:1, so gain is zero). The inventory is more accurate.
- **What this requires in code**: `handleFunding` calls `Lot.dispose` for the USDC amount. Error handling for insufficient USDC inventory. Possibly a fee-like treatment instead of a full disposal.
- **What this requires in docs**: Document the accounting treatment. Decide whether the disposal rows belong on 8949 (USDC disposals are generally zero-gain but still reportable in some interpretations).
- **Risk**: This is a tax-semantic decision. Treating funding expense as a USDC disposal is one valid interpretation but not the only one. Requires human decision.

## Tradeoffs

| | Structured output? | Inventory effect? | Tax semantics required? | Blocked by output-channel decision? | Implementation size |
| --- | --- | --- | --- | --- | --- |
| **A** (status quo) | No | No | No | No | None |
| **B** (stderr summary) | No | No | No | No | Small |
| **C** (second report) | Yes | No | Partially | Yes | Medium-large |
| **D** (lot consumption) | Yes (8949 rows) | Yes | Yes | No | Medium |

## Smallest honest next step

**Option B** is the smallest change that improves visibility without requiring a tax-semantic decision, an output-channel decision, or evidence verification.

It can be implemented independently of all other blocked work. It does not change any existing output. It does not overclaim support. It makes the expense total visible to the operator in the same place where the income total already appears.

Options C and D both depend on decisions that are not yet made (the non-8949 output channel and tax semantics respectively). They should wait for those decisions.

## What remains blocked

Even after choosing an output policy:

- **Evidence verification**: The real-wallet funding exemplars (`hl-funding-negative-2025-10-07`, `hl-funding-positive-2025-12-02`) still need human review before any support-boundary upgrade. This is task `verification.expand_human_verified_exemplars` in the DAG.
- **Market context preservation**: The normalized IR drops the Hyperliquid market that produced the funding. Fixing this requires the `Funding rows lose market context and still have weak evidence linkage` review item in `docs/hyperliquid/REVIEW.md` to be addressed, plus the broader `decision.ir_contract_refinement` DAG node if the IR contract must change.
- **Evidence linkage**: The all-zero source hash makes individual funding rows hard to audit. Improving this requires either a richer identifier from the API or a synthetic key.
- **Non-8949 output channel**: If the decision is to emit structured expense output (Option C), that depends on the `The core has no stable output channel for modeled income or unsupported rows` review item in `docs/core/REVIEW.md`.
- **Tax semantics for lot consumption**: If the decision is to deduct from USDC inventory (Option D), that requires professional tax guidance or at minimum explicit human sign-off on the accounting interpretation.

## Accounting truth vs presentation choice

This distinction matters for the decision:

- **Accounting truth**: Negative funding is an economic USDC outflow. The user's USDC balance decreased. This is a fact regardless of output policy. The pipeline already preserves this fact through normalization and surfaces it as an unsupported expense.

- **Presentation choice**: Whether and how that outflow appears in final output is a design decision. The pipeline could present it as a stderr summary (Option B), a structured expense report (Option C), a USDC disposal (Option D), or not at all beyond a warning (Option A). None of these presentation choices change the underlying accounting truth. They change what information reaches the user in what form.

The current gap is purely a presentation gap, not a data gap. The accounting fact is already captured in the IR. The question is how far downstream it should travel in structured form.
