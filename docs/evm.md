# EVM (Ethereum, Arbitrum)

## Intent

- Every row Etherscan has for a transaction (normal, internal, ERC-20,
  ERC-721/1155) is netted into one movement per address. Internal
  transactions matter: DEX and bridge outputs in ETH arrive only there.
- The wallet signed a transaction only if it sent the normal transaction;
  only then does it pay the gas (`gasUsed × gasPrice`, which on Arbitrum
  already includes the L1 component). Failed transactions keep their gas.
- Only chains whose gas coin is ETH are supported.

## Quirks

- Etherscan's free API key refuses Base, Optimism, BSC and Avalanche ("Free
  API access is not supported for this chain"). Before trusting that a wallet
  is ETH/Arbitrum-only, check each chain's nonce via a public RPC.
- Rate limiting comes back as HTTP 200 with an error message in the body; it
  must be retried, never cached as data.
- Address-poisoning contracts emit fake `Transfer(from = you, …)` events for
  tokens you never held. They appear as unsigned outflows; balance
  reconciliation stays exact only because they are dropped.
- Etherscan refuses `page × offset` beyond 10,000 rows per query; larger
  histories need block-window restarts.
