# Assets — identity and valuation

## Intent

- **Identity comes from the source, never from a symbol.** An asset is its
  chain and contract address (or Solana mint). Symbols are attacker-controlled
  display text: address-poisoning tokens are named `USDC` (sometimes with
  homoglyphs) and would otherwise inherit real USDC's lots and $1 price.
- A small verified table maps identities to economic assets. Wrapped native
  coins (WETH, wSOL) are the native coin, so wrapping is not a taxable
  exchange. USD stablecoins (USDC, USDC.e, USDT, DAI, PYUSD on the supported
  chains, and Hyperliquid's USDC) are the single cash asset `USD`. Every
  address in the table was checked against an external source (DefiLlama
  symbol lookup).
- Form 1099-DA rows are identified by their DTI code; an unknown code fails
  instead of being guessed from the asset's name.
- **Valuation**: cash is worth its face value. Everything else is priced by
  DefiLlama's historical API using the same identity. A quote counts only if
  it lies within 2 hours of the instant and has confidence ≥ 0.9. Low
  confidence is exactly where spam tokens get fake prices. Exchange-reported
  values (perp PnL, broker proceeds) are used as given.
- Unknown tokens print as a defanged symbol plus an address suffix, or as
  their full mint when the source has no symbol.

## Quirks

- CoinGecko's free tier only serves the last 365 days of history, so it
  cannot price 2023–2024; that is why DefiLlama is the oracle.
- DTI codes are easy to misremember (Ethereum is `XB0MQJ1K5`). Add a code
  only from a broker form or the DTI registry.
