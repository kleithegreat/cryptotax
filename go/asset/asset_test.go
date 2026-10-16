package asset

import "testing"

func TestIdentityComesFromAddressesNotSymbols(t *testing.T) {
	cases := []struct {
		chain, addr, symbol, key string
	}{
		{"arbitrum", "0xAF88d065e77c8cC2239327C5EDb3A432268e5831", "USDC", Cash},
		{"arbitrum", "0x00000000000000000000000000000000000fa4e0", "USDC", "arbitrum:0x00000000000000000000000000000000000fa4e0"},
		{"ethereum", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "WETH", "ETH"},
		{"solana", "So11111111111111111111111111111111111111112", "", "SOL"},
		{"solana", "CmMnjETQsdr79xaLKttgQjaqwUWqzULifLJt8F7mpump", "", "solana:CmMnjETQsdr79xaLKttgQjaqwUWqzULifLJt8F7mpump"},
	}
	for _, c := range cases {
		if got := Token(c.chain, c.addr, c.symbol).Key; got != c.key {
			t.Errorf("Token(%s, %s, %q) = %s, want %s", c.chain, c.addr, c.symbol, got, c.key)
		}
	}
}

func TestDisplaySymbolIsDefanged(t *testing.T) {
	a := Token("arbitrum", "0x00000000000000000000000000000000000fa4e0", "ꓴꓢꓓС | t.me/claim")
	if a.Symbol != "???????t?me?~0fa4e0" {
		t.Errorf("symbol %q", a.Symbol)
	}
	if b := Token("solana", "Mint1111", ""); b.Symbol != "Mint1111" {
		t.Errorf("unnamed token should print its mint, got %q", b.Symbol)
	}
}
