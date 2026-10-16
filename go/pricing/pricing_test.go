package pricing

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kevin/cryptotax/netcache"
)

func TestQuotesMustBeCloseAndConfident(t *testing.T) {
	dir := t.TempDir()
	at := time.Unix(1700000000, 0)
	quote := func(id, body string) {
		path := filepath.Join(dir, "price", id, "1700000000.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	quote("coingecko_solana", `{"coins":{"coingecko:solana":{"price":150.25,"timestamp":1700000060,"confidence":0.99}}}`)
	quote("solana_Thin", `{"coins":{"solana:Thin":{"price":3,"timestamp":1700000000,"confidence":0.4}}}`)
	quote("solana_Stale", `{"coins":{"solana:Stale":{"price":3,"timestamp":1690000000,"confidence":0.99}}}`)
	p := New(netcache.New(dir, true))

	if v, ok := p.Unit("SOL", at); !ok || v.FloatString(2) != "150.25" {
		t.Errorf("SOL = %v %v", v, ok)
	}
	if _, ok := p.Unit("solana:Thin", at); ok {
		t.Error("a low-confidence quote is not a price")
	}
	if _, ok := p.Unit("solana:Stale", at); ok {
		t.Error("a quote from another day is not a price")
	}
	if v, ok := p.Unit("USD", at); !ok || v.RatString() != "1" {
		t.Error("cash is worth its face value")
	}
	if _, ok := p.Unit("ethereum:0xnft#1", at); ok {
		t.Error("NFTs have no fungible price")
	}
}
