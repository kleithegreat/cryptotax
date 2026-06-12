# EVM Architecture

## Purpose

The current EVM pipeline fetches Ethereum and Arbitrum account history from Etherscan, converts each returned movement into a conservative normalized row, and then runs a narrow transfer-matching pass for own-wallet transfers and one confirmed bridge pattern.

## Current ownership boundary

- `go/fetcher/etherscan.go` owns Etherscan ingestion.
- `go/normalize/normalize.go` owns EVM normalization in `normalizeEVM`.
- `go/transfer/match.go` owns post-normalization transfer matching.
- `go/audit/testdata/real-wallet` and `docs/known-transactions.md` hold the current-behavior checkpoints for real-wallet EVM cases.
- The code does not currently own general swap reconstruction, trace-level bridge reconstruction, or contract-level semantic grouping.

## Main files and named constructs

- `go/fetcher/etherscan.go`
- `go/normalize/normalize.go`
- `go/transfer/match.go`
- `go/fetcher/etherscan_test.go`
- `go/transfer/match_test.go`

Important named constructs:

- `Etherscan`
- `Fetch`
- `fetchEndpoint`
- `fetchPage`
- `parseEtherscanResult`
- `normalizeEVM`
- `classifyAddressMovement`
- `MatchTransfers`
- `isOwnWalletTransferPair`
- `isConfirmedBridgeTransferPair`

## Data flow

- `buildPayload` in `go/cmd/main.go` creates two `Etherscan` fetchers per `--eth-wallet`: one for `types.ChainEthereum` and one for `types.ChainArbitrum`.
- `Etherscan.Fetch` requests both `txlist` and `tokentx` through `fetchEndpoint`.
- `fetchPage` paginates ascending by page number, retries rate-limit failures, skips rows where `IsError == "1"`, and converts the Etherscan payload into `fetcher.RawTransaction`.
- Native rows use `Asset = "ETH"` and `Amount = weiToEther(Value)`. ERC-20 rows use `Asset = TokenSymbol` and `Amount = tokenToDecimal(Value, TokenDecimal)`.
- Every raw EVM row carries `Wallet`, `FromAddr`, `ToAddr`, `RawType = FunctionName`, and `Fee = weiToEther(GasUsed * GasPrice)`.
- `normalizeEVM` classifies each row with `classifyAddressMovement`. Wallet-originating rows usually become `sell` unless the destination is another owned wallet. Wallet-receiving rows become `transfer_in`.
- `normalizeEVM` attaches the fee only to rows where `classifyAddressMovement` sets `attachFee = true`, which is currently the outbound side.
- `MatchTransfers` then performs a second pass. It preserves true own-wallet transfer pairs and contains one evidence-driven bridge exception that can relabel an outbound `sell` to `transfer_out` when it matches a cross-chain `fillRelay` inbound row.

## Outputs / side effects

- EVM rows enter the IR with `source="etherscan"` and `chain="ethereum"` or `chain="arbitrum"`.
- `raw_type` is whatever Etherscan returned in `FunctionName`.
- Fees stay visible on outbound rows even when the economic meaning of the main row is still unresolved.
- The bridge snapshot fixture in `go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json` documents one captured behavior set.

## Current support boundary

### Implemented behavior

- Etherscan ingestion for native transfers and ERC-20 transfer rows is implemented.
- Conservative row-by-row normalization is implemented.
- Same-asset own-wallet transfer matching is implemented when both sides explicitly point at owned wallets.
- The confirmed bridge matcher is implemented for one exact EVM bridge pattern.

### Current-behavior-only checkpoints

- `go/fetcher/etherscan_test.go` freezes pagination, empty-response handling, and rate-limit retry behavior.
- `go/transfer/match_test.go` freezes the current own-wallet and confirmed-bridge matching rules.
- `go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json` documents bridge normalization as of its capture snapshot (it does not re-run the normalizer).
- `docs/known-transactions.md` records additional EVM review targets such as `eth-swap-usdc` and suspicious-asset receipts.

### Unsupported but surfaced behavior

- Multi-leg swaps remain visible as separate conservative rows instead of one reconstructed swap event.
- General bridge inference is unsupported beyond the hard-coded confirmed pattern.
- Zero-amount artifacts stay visible when the parser emitted them.
- Inbound contract outputs stay as `transfer_in` even when they may later turn out to be income, proceeds, or one leg of a larger interaction.

## Current known approximations or conservative behavior

- The fetch path only uses `txlist` and `tokentx`. There is no trace-level or event-level reconstruction step.
- ERC-20 identity in normalized rows is the Etherscan `TokenSymbol`. Contract-address identity is not preserved in the current IR.
- `classifyAddressMovement` treats many wallet-originating rows as `sell` because it only reasons over addresses and ownership, not contract semantics.
- The bridge exception in `MatchTransfers` is intentionally narrow. It requires exact raw-type prefixes, exact counterparty addresses, different chains, the same wallet on both sides, the same asset, and close timestamps and amounts.

## Notable current divergences from spec

None beyond the deliberately narrow and documented conservative boundary above.
