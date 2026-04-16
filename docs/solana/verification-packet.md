# Solana Verification Packet

Compact human-verification packet for pending Solana exemplar cases.

Grounded in: `docs/known-transactions.md`, `docs/solana/SPEC.md`, `docs/solana/ARCHITECTURE.md`, `docs/solana/REVIEW.md`

## How to use

For each case:

1. Open the source tx on a Solana explorer
2. Compare the explorer instruction trace to the current normalized description below
3. Answer the exact unresolved question
4. Record the verdict back in `docs/known-transactions.md`
5. If the case is clear enough to freeze, copy the filtered payload into `go/audit/testdata/real-wallet/`

---

## Case 1: sol-pumpfun-zero-usd

| Field | Value |
| --- | --- |
| Label | `sol-pumpfun-zero-usd` |
| Source system | Helius enhanced transactions / Solana explorer |
| Source tx | `PbxPFcX7JQF6PuTMjRs2xKuC2azpAmnc1uALwYxCKuwnWFT3vb156p17CZRYjcU1ySB1ANHSWgGfPEfHtE2QXKm` |
| Wallet | `BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp` |
| Current normalized | 1 row; swap sent SOL 0.000803279 @ 0.12734483 USD, received `CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP` 540724.686218000 @ 0 USD, fee SOL 0.001005000 @ 0.15932391 USD |
| Regression fixtures | `sol-pumpfun-zero-usd.input.json`, `solana-pumpfun-zero-usd.expected.json` |
| Blocker category | **Identity** + **Valuation** |

**Exact unresolved question:**

1. What is the received mint `CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP`? Identify its symbol/name on the explorer.
2. Is the swap classification correct per the explorer instruction trace?
3. Should the zero USD valuation stand as the permanent conservative stance (no supported pricing path for this token), or does a valuation source need to be adopted?

**Support-boundary upgrade unlocked:** Resolving this case grounds mint identity for pump.fun-sourced tokens and establishes whether zero-USD for unpriced mints is acceptable or a gap to close. The `asset_canonical` schema checkpoint is already implemented; this case now feeds into `solana.identity_valuation_support_boundary_upgrade` plus the remaining valuation-surfacing review work in `docs/solana/REVIEW.md`.

---

## Case 2: sol-dflow-swap

| Field | Value |
| --- | --- |
| Label | `sol-dflow-swap` |
| Source system | Helius enhanced transactions / Solana explorer |
| Source tx | `gcjrtf2z4ZKat5oq14ZyJG7Sk4D4M6ndTfo5arcLzPr5ChgTZ4HcERmYBbHwBrqbu9wBk6V6y711n1uu5SuT2TJ` |
| Wallet | `BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp` |
| Current normalized | 1 row; swap sent SOL 2.086215720 @ 487.83195732 USD, received SOL 0.002039280 @ 0.47685670 USD, fee SOL 0.000080001 @ 0.01870710 USD |
| Regression fixtures | None |
| Blocker category | **Economic interpretation** |

**Exact unresolved question:**

1. Is this a genuine taxable SOL-for-SOL swap through a DEX?
2. Or is this a DFlow aggregator routing artifact where the small received SOL is change, rent reclaim, or an internal account refund?
3. Or is this a transfer/fee pattern that should not produce a disposal event at all?

The explorer instruction trace should clarify whether funds were routed through a DEX swap or are internal account movement.

**Support-boundary upgrade unlocked:** Resolving this case establishes how the pipeline should handle same-asset swap-like rows from aggregator routing. Feeds into `solana.identity_valuation_support_boundary_upgrade` and the `Current multi-row Solana output cannot clearly separate conservative preservation from routing artifacts` review item in `docs/solana/REVIEW.md`.

---

## Case 3: sol-addresslike-mint

| Field | Value |
| --- | --- |
| Label | `sol-addresslike-mint` |
| Source system | Helius enhanced transactions / Solana explorer |
| Source tx | `2dh7RefWvkHAKL9YQ1wZx5DJnhYMGWNkz2xSTh86792TgzhDPZiYw3VivrM6GdwMfJbWYNihZByo5ujEtXHBmQUn` |
| Wallet | `BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp` |
| Current normalized | 10 rows with mixed sell/transfer_in legs across SOL, wrapped SOL mint `SO111...`, and address-like mint `EPJFWDD5AUFQSSQEM2QN1XZYBAPC8G4WEGGKZWYTDT1V`; several legs have zero USD values |
| Regression fixtures | None |
| Blocker category | **Identity** + **Valuation** + **Economic interpretation** |

**Exact unresolved questions:**

1. What is the mint `EPJFWDD5AUFQSSQEM2QN1XZYBAPC8G4WEGGKZWYTDT1V`? Identify it on the explorer.
2. Is this transaction one economic swap, multiple internal account moves, or a combination?
3. Which of the 10 rows represent real economic legs with value, and which are zero-amount bookkeeping artifacts?
4. Is the 10-row explosion an acceptable conservative representation, or should it collapse into fewer economic rows?

**Support-boundary upgrade unlocked:** This is the hardest of the three cases. Resolving it grounds multi-leg transaction handling, validates or rejects the current row-explosion behavior, and establishes identity for the most common address-like mint in the dataset. The `asset_canonical` plus `event_group_id` / `split_reason` plumbing is already in place; this case now feeds into the remaining Solana review gaps around unresolved valuation and ambiguous multi-row interpretation, plus `solana.identity_valuation_support_boundary_upgrade`.

---

## Summary matrix

| Case | Identity | Valuation | Econ. interpretation | Fixture exists | Primary unlock |
| --- | --- | --- | --- | --- | --- |
| `sol-pumpfun-zero-usd` | blocker | blocker | - | yes | mint identity, zero-USD policy |
| `sol-dflow-swap` | - | - | blocker | no | same-asset routing artifact policy |
| `sol-addresslike-mint` | blocker | blocker | blocker | no | multi-leg handling, address-like mint ID |

## Relationship to TASK_DAG

These three cases are the Solana subset of `verification.expand_human_verified_exemplars` (`needs_human_verification`). Human resolution of all three is still required before `solana.identity_valuation_support_boundary_upgrade` can unblock. The earlier IR schema checkpoint (`decision.ir_contract_refinement`) and core accounting checkpoint (`core.accounting_support_upgrade`) are already done; the remaining Solana work is evidence-backed support-boundary cleanup, not baseline contract plumbing.

## After human review

For each resolved case:

1. Update the matching entry in `docs/known-transactions.md` with the verdict
2. If clear enough, freeze a regression fixture in `go/audit/testdata/real-wallet/`
3. Once all three are resolved, `verification.expand_human_verified_exemplars` can progress for the Solana domain
