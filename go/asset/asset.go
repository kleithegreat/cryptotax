// Package asset decides what a token IS. Lots, pricing and transfer pairing
// all key on Asset.Key, which comes only from source-backed identity
// (contract address, mint, chain-native coin) — never from a symbol, because
// symbols are attacker-controlled (address-poisoning tokens call themselves
// "USDC"). See docs/assets.md.
package asset

import (
	"strings"
	"unicode"
)

// Asset is an economic asset. Key is the identity; Symbol is display only.
type Asset struct {
	Key    string `json:"asset"`
	Symbol string `json:"symbol"`
}

// Cash is the single key for US-dollar stablecoins; they carry no lots.
const Cash = "USD"

// IsCash reports whether a key is the USD cash asset.
func IsCash(key string) bool { return key == Cash }

var (
	usd = Asset{Cash, "USD"}
	eth = Asset{"ETH", "ETH"}
	sol = Asset{"SOL", "SOL"}
	btc = Asset{"BTC", "BTC"}
)

// known maps source identities ("chain:address", lowercased for EVM) to
// economic assets. Wrapped native coins share the native asset: wrapping is
// treated as non-taxable (docs/assets.md).
var known = map[string]Asset{
	"ethereum:0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": usd, // USDC
	"ethereum:0xdac17f958d2ee523a2206206994597c13d831ec7": usd, // USDT
	"ethereum:0x6b175474e89094c44da98b954eedeac495271d0f": usd, // DAI
	"ethereum:0x6c3ea9036406852006290770bedfcaba0e23a0e8": usd, // PYUSD
	"ethereum:0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2": eth, // WETH
	"arbitrum:0xaf88d065e77c8cc2239327c5edb3a432268e5831": usd, // USDC
	"arbitrum:0xff970a61a04b1ca14834a43f5de4533ebddb5cc8": usd, // USDC.e
	"arbitrum:0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9": usd, // USDT
	"arbitrum:0xda10009cbd5d07dd0cecc66161fc93d7c9000da1": usd, // DAI
	"arbitrum:0x82af49447d8a07e3bd95bd0d56f35241523fbab1": eth, // WETH
	"solana:EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v": usd, // USDC
	"solana:Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB": usd, // USDT
	"solana:2b1kV6DkPAnxd5ixfnxCpjxmKwqjjaYmCZfHsFu24GXo": usd, // PYUSD
	"solana:So11111111111111111111111111111111111111112":  sol, // wSOL
}

// Native returns the gas coin of a chain.
func Native(chain string) Asset {
	if chain == "solana" {
		return sol
	}
	return eth
}

// Token resolves a token by its source identity. Unknown tokens keep their
// chain-qualified identity as the key; the source symbol is only decoration.
func Token(chain, address, symbol string) Asset {
	if chain != "solana" {
		address = strings.ToLower(address)
	}
	key := chain + ":" + address
	if a, ok := known[key]; ok {
		return a
	}
	return Asset{Key: key, Symbol: displaySymbol(symbol, address)}
}

// HyperliquidUSDC is the collateral token of Hyperliquid.
func HyperliquidUSDC() Asset { return usd }

// dtis maps Digital Token Identifiers (as printed on Form 1099-DA) to assets.
// Only codes verified against a source are listed; anything else must fail.
var dtis = map[string]Asset{
	"20J63Z4N3": sol, // Robinhood 1099-DA "Solana SOL"
	"CK9PW1MFH": usd, // Robinhood 1099-DA "USDC" (flagged STABLECOIN)
	"4H95J0R2X": btc,
	"XB0MQJ1K5": eth,
}

// ByDTI resolves a Form 1099-DA DTI code.
func ByDTI(code string) (Asset, bool) {
	a, ok := dtis[strings.TrimSpace(code)]
	return a, ok
}

// displaySymbol makes an untrusted source symbol safe to print: scam tokens
// embed URLs and homoglyphs, so anything non-ASCII-alphanumeric is replaced
// and the address suffix is appended to keep distinct tokens distinguishable.
// Without a symbol the address itself is the name.
func displaySymbol(symbol, address string) string {
	if symbol == "" {
		return address
	}
	var b strings.Builder
	for _, r := range symbol {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		} else {
			b.WriteRune('?')
		}
		if b.Len() >= 12 {
			break
		}
	}
	s := b.String()
	tail := address
	if len(tail) > 6 {
		tail = tail[len(tail)-6:]
	}
	return s + "~" + tail
}

// Contracts lists the token contracts (or mints) on chain whose units count
// as the asset key, for balance reconciliation.
func Contracts(chain, key string) []string {
	var out []string
	for id, a := range known {
		if c, addr, _ := strings.Cut(id, ":"); c == chain && a.Key == key {
			out = append(out, addr)
		}
	}
	if c, addr, ok := strings.Cut(key, ":"); ok && c == chain && !strings.Contains(addr, "#") {
		out = append(out, addr)
	}
	return out
}
