# Financial Core Architecture

## Purpose

The Haskell core consumes normalized JSON from stdin, applies FIFO lot accounting, and writes the current tax-oriented CSV output. Its executable entry point is `haskell/app/Main.hs`.

## Current ownership boundary

- `haskell/src/Types.hs` owns the Haskell mirror of the JSON IR plus the internal `TaxLot`, `Disposal`, and `GainLoss` types.
- `haskell/src/Lot.hs` owns FIFO lot queue updates.
- `haskell/src/GainLoss.hs` owns transaction-by-transaction accounting over normalized rows.
- `haskell/src/Report.hs` owns the current CSV renderer.
- The core does not fetch source data, infer wallet ownership, or reconstruct source-specific transaction meaning.

## Main files and named constructs

- `haskell/app/Main.hs`
- `haskell/src/Types.hs`
- `haskell/src/Lot.hs`
- `haskell/src/GainLoss.hs`
- `haskell/src/Report.hs`
- `haskell/test/Spec.hs`

Important named constructs:

- `parseDecimal`
- `TaxLot`
- `Disposal`
- `GainLoss`
- `LotQueue`
- `acquire`
- `dispose`
- `processTransactions`
- `ProcessResult`
- `render8949CSV`

## Data flow

- `Main.main` reads the normalized payload from stdin and decodes it with `Aeson.eitherDecode` into `TxPayload`.
- `processTransactions` sorts rows by `txTimestamp` and folds them through `processTx`.
- `handleBuy` reads `txReceived`, parses `aaAmount` and `aaUSDValue` with `parseDecimal`, adds `feeUSD`, and calls `Lot.acquire` to append a new FIFO lot.
- `handleSell` builds a `Disposal` from `txSent` and `feeUSD`, then calls `Lot.dispose` to consume existing lots and emit `GainLoss` rows.
- `handleSwap` also builds a `Disposal` from `txSent`, then acquires a new lot for `txReceived` after the disposal succeeds.
- `handleIncome` routes inbound `income` rows to `recordIncomeReceipt`, which both creates a lot at fair market value and records an entry in `prIncome`.
- `handleFunding` treats positive `funding_payment` rows as income receipts and treats negative `funding_payment` rows as unsupported expenses by appending an error string to `prErrors`.
- `TransferIn` and `TransferOut` rows currently leave the accumulator unchanged.
- `render8949CSV` renders only `prGainLosses` to CSV.

## Outputs / side effects

- `Main.main` writes the CSV file selected by `--output`.
- `Main.main` writes warning lines for every entry in `prErrors`.
- `Main.main` prints aggregate gain/loss and aggregate income totals to stderr.
- The only structured file output today is the 8949-oriented CSV rendered by `render8949CSV`.

## Current support boundary

### Implemented behavior

- Exact decimal math stays in `Rational` form through `parseDecimal`, `TokenAmount`, and `USD`.
- FIFO ordering lives in `Lot.acquire`, `Lot.dispose`, and `consumeLots`.
- `mkGain` and `holdingPeriod` produce per-lot disposal rows with exact basis and proceeds allocation.
- The local golden fixture in `haskell/testdata/basic-buy-sell-transfer.json` and `haskell/testdata/basic-buy-sell-transfer-8949.csv` is the strongest current end-to-end proof of supported output.

### Current-behavior-only checkpoints

- `haskell/test/Spec.hs` freezes the current accounting behavior with QuickCheck properties and the golden fixture.
- The positive and negative funding properties in `haskell/test/Spec.hs` freeze current Hyperliquid funding handling, but they do not upgrade negative funding expense semantics to supported output.

### Unsupported but surfaced behavior

- Insufficient inventory becomes a `prErrors` entry from `Lot.dispose`.
- Negative `funding_payment` rows remain explicit unsupported errors instead of being converted into fake spot semantics.
- Any upstream approximation that already arrived as `buy`, `sell`, or `swap` is processed as-is. The core does not recover lost source semantics.

## Current known approximations or conservative behavior

- `Swap` is treated as one disposal plus one acquisition using the upstream USD legs already present in the IR.
- `recordIncomeReceipt` captures income economically, but `render8949CSV` does not emit a separate income report. Income is only accumulated in `prIncome` and summarized to stderr.
- `TransferIn` and `TransferOut` currently do not affect lots or final output.

## Notable current divergence from spec

- `processTx` drops every `TransferIn` and `TransferOut` row without an error or explicit unsupported output. That means conservative upstream transfer-like rows do not remain visible once they reach the core. This is a concrete mismatch with the spec's surfacing requirement and likely belongs in a future `docs/core/REVIEW.md`.
