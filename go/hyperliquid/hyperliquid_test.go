package hyperliquid

import (
	"encoding/json"
	"testing"
)

const me = "0x00000000000000000000000000000000000000aa"

func TestFlipFillClosesThenOpens(t *testing.T) {
	var res Result
	raw := json.RawMessage(`{"coin":"BTC","px":"100000","sz":"0.3","time":1760000000000,"startPosition":"0.1","dir":"Long > Short","closedPnl":"12.5","hash":"0xh","fee":"0.9","feeToken":"USDC","tid":7}`)
	if err := res.fill(raw, "hyperliquid:"+me); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || len(res.Issues) != 0 {
		t.Fatalf("events %+v issues %+v", res.Events, res.Issues)
	}
	p := res.Events[0].Perp
	if p.Close == nil || p.Close.Side != "long" || p.Close.Size != "0.1" ||
		p.Open == nil || p.Open.Side != "short" || p.Open.Size != "0.2" || p.PnL != "12.5" || p.Fee != "0.9" {
		t.Errorf("fill %+v close %+v open %+v", p, p.Close, p.Open)
	}
}

func TestUnknownFillDirectionIsAnIssue(t *testing.T) {
	var res Result
	raw := json.RawMessage(`{"coin":"@107","px":"1","sz":"1","time":1,"startPosition":"0","dir":"Buy","closedPnl":"0","hash":"0xh","fee":"0","feeToken":"USDC","tid":8}`)
	if err := res.fill(raw, "hyperliquid:"+me); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 0 || len(res.Issues) != 1 || !res.Issues[0].Blocking {
		t.Errorf("spot fills must surface, not be guessed: %+v %+v", res.Events, res.Issues)
	}
}

func TestLedgerDepositSharesTheArbitrumTxID(t *testing.T) {
	var res Result
	for _, raw := range []string{
		`{"time":1,"hash":"0xdep","delta":{"type":"deposit","usdc":"50.0"}}`,
		`{"time":2,"hash":"0xwd","delta":{"type":"withdraw","usdc":"20.5","fee":"1.0"}}`,
		`{"time":3,"hash":"0xc","delta":{"type":"accountClassTransfer","usdc":"5","toPerp":true}}`,
		`{"time":4,"hash":"0xs","delta":{"type":"send","user":"0xother","destination":"` + me + `","token":"USDC","amount":"7","fee":"0"}}`,
		`{"time":5,"hash":"0xv","delta":{"type":"vaultDeposit","vault":"0xv","usdc":"10"}}`,
	} {
		if err := res.ledger(json.RawMessage(raw), me, "hyperliquid:"+me); err != nil {
			t.Fatal(err)
		}
	}
	if len(res.Movements) != 3 {
		t.Fatalf("movements %+v", res.Movements)
	}
	dep, wd, send := res.Movements[0], res.Movements[1], res.Movements[2]
	if dep.ID != "arbitrum:0xdep" || dep.Deltas[0].Amount.RatString() != "50" {
		t.Errorf("deposit %+v", dep)
	}
	if wd.ID != "arbitrum:0xwd" || wd.Deltas[0].Amount.RatString() != "-41/2" || wd.Fee.Amount.RatString() != "1" {
		t.Errorf("withdraw %+v", wd)
	}
	if send.ID != "hyperliquid:0xs" || send.Signed || send.Counterparties[0] != "0xother" {
		t.Errorf("send %+v", send)
	}
	if len(res.Issues) != 1 || !res.Issues[0].Blocking {
		t.Errorf("unsupported ledger types must block: %+v", res.Issues)
	}
}
