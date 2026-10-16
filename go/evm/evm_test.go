package evm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/netcache"
)

const (
	w    = "0x00000000000000000000000000000000000000aa"
	usdc = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	fake = "0x00000000000000000000000000000000000fa4e0"
)

func seed(t *testing.T, dir, action, result string) {
	t.Helper()
	path := filepath.Join(dir, "evm", "ethereum", w, action, "0-1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMovementsNetEveryRowOfATransaction(t *testing.T) {
	dir := t.TempDir()
	empty := `{"status":"0","message":"No transactions found","result":[]}`
	seed(t, dir, "txlist", `{"status":"1","message":"OK","result":[
	  {"hash":"0xswap","timeStamp":"1700000000","blockNumber":"1","from":"`+w+`","to":"0xrouter","value":"0","isError":"0","gasUsed":"100000","gasPrice":"10000000000","functionName":"swap(address)"},
	  {"hash":"0xfail","timeStamp":"1700000100","blockNumber":"2","from":"`+w+`","to":"0xrouter","value":"1000000000000000000","isError":"1","gasUsed":"21000","gasPrice":"10000000000","functionName":""},
	  {"hash":"0xin","timeStamp":"1700000200","blockNumber":"3","from":"0xfriend","to":"`+w+`","value":"500000000000000000","isError":"0","gasUsed":"21000","gasPrice":"1"}]}`)
	seed(t, dir, "txlistinternal", `{"status":"1","message":"OK","result":[
	  {"hash":"0xswap","timeStamp":"1700000000","blockNumber":"1","from":"0xrouter","to":"`+w+`","value":"50000000000000000","isError":"0","traceId":"0_1"}]}`)
	seed(t, dir, "tokentx", `{"status":"1","message":"OK","result":[
	  {"hash":"0xswap","timeStamp":"1700000000","blockNumber":"1","from":"`+w+`","to":"0xpool","value":"100000000","contractAddress":"`+usdc+`","tokenSymbol":"USDC","tokenDecimal":"6","logIndex":"3"},
	  {"hash":"0xpoison","timeStamp":"1700000300","blockNumber":"4","from":"`+w+`","to":"0xlookalike","value":"100000000","contractAddress":"`+fake+`","tokenSymbol":"USDC","tokenDecimal":"6","logIndex":"1"}]}`)
	seed(t, dir, "tokennfttx", empty)
	seed(t, dir, "token1155tx", empty)

	f := &Fetcher{Net: netcache.New(dir, true)}
	ms, err := f.Movements("ethereum", w)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		signed bool
		deltas map[string]string
		fee    string
	}
	wants := map[string]want{
		"ethereum:0xswap":   {true, map[string]string{"ETH": "0.05", "USD": "-100"}, "0.001"},
		"ethereum:0xfail":   {true, map[string]string{}, "0.00021"},
		"ethereum:0xin":     {false, map[string]string{"ETH": "0.5"}, ""},
		"ethereum:0xpoison": {false, map[string]string{"ethereum:" + fake: "-100"}, ""},
	}
	if len(ms) != len(wants) {
		t.Fatalf("got %d movements", len(ms))
	}
	for _, m := range ms {
		wt, ok := wants[m.ID]
		if !ok {
			t.Fatalf("unexpected %s", m.ID)
		}
		got := map[string]string{}
		for _, d := range m.Deltas {
			got[d.Asset.Key] = decimal.String(d.Amount)
		}
		fee := ""
		if m.Fee != nil {
			fee = decimal.String(m.Fee.Amount)
		}
		if m.Signed != wt.signed || fee != wt.fee || len(got) != len(wt.deltas) {
			t.Errorf("%s: signed=%v fee=%q deltas=%v", m.ID, m.Signed, fee, got)
		}
		for k, v := range wt.deltas {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", m.ID, k, got[k], v)
			}
		}
	}
}
