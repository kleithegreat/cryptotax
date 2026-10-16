package ledger

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/config"
)

type fixedPrices map[string]*big.Rat

func (p fixedPrices) Value(key string, amount *big.Rat, _ time.Time) (*big.Rat, bool) {
	if asset.IsCash(key) {
		return new(big.Rat).Abs(amount), true
	}
	unit, ok := p[key]
	if !ok {
		return nil, false
	}
	v := new(big.Rat).Abs(amount)
	return v.Mul(v, unit), true
}

var (
	prices = fixedPrices{"ETH": big.NewRat(2000, 1), "SOL": big.NewRat(150, 1)}
	t0     = time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	eth    = asset.Asset{Key: "ETH", Symbol: "ETH"}
	sol    = asset.Asset{Key: "SOL", Symbol: "SOL"}
	usd    = asset.Asset{Key: "USD", Symbol: "USD"}
	junk   = asset.Asset{Key: "ethereum:0xjunk", Symbol: "JUNK~0xjunk"}
)

func d(a asset.Asset, s string) Delta {
	r, _ := new(big.Rat).SetString(s)
	return Delta{Asset: a, Amount: r}
}

func mv(id string, at time.Duration, account string, signed bool, cps []string, ds ...Delta) Movement {
	return Movement{ID: id, Time: t0.Add(at), Account: account, Deltas: ds, Signed: signed, Counterparties: cps}
}

func cfg(cps map[string]config.Counterparty, ovs map[string]config.Override) *config.Config {
	return &config.Config{Location: time.UTC, FirstYear: 2025, Counterparties: cps, Overrides: ovs}
}

func find(t *testing.T, p Payload, id string) Event {
	t.Helper()
	for _, e := range p.Events {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no event %s in %v", id, ids(p))
	return Event{}
}

func ids(p Payload) []string {
	var out []string
	for _, e := range p.Events {
		out = append(out, e.ID+"="+string(e.Kind)+"/"+e.Reason)
	}
	return out
}

func blocking(p Payload) int {
	n := 0
	for _, i := range p.Issues {
		if i.Blocking {
			n++
		}
	}
	return n
}

func TestSpamDustAndSpoofs(t *testing.T) {
	p := Build(cfg(nil, nil), []Movement{
		mv("e:airdrop", 0, "ethereum:me", false, []string{"0xscam"}, d(junk, "1000")),
		mv("s:dust", time.Minute, "solana:me", false, []string{"poisoner"}, d(sol, "0.000001")),
		mv("e:spoof", 2*time.Minute, "ethereum:me", false, []string{"0xlookalike"}, d(junk, "-5")),
		mv("e:drain", 3*time.Minute, "ethereum:me", false, []string{"0xthief"}, d(eth, "-1")),
	}, nil, nil, prices)

	for _, id := range []string{"e:airdrop", "s:dust"} {
		if e := find(t, p, id); e.Kind != Add || e.Reason != "spam" || e.Basis == nil || *e.Basis != "0" {
			t.Errorf("%s: %+v", id, e)
		}
	}
	for _, e := range p.Events {
		if e.ID == "e:spoof" {
			t.Errorf("spoofed outflow must not become an event")
		}
	}
	if e := find(t, p, "e:drain"); e.Kind != Remove || e.Reason != "unresolved" {
		t.Errorf("an unsigned outflow of real value is unresolved, got %+v", e)
	}
	if blocking(p) != 1 {
		t.Errorf("blocking = %d, want 1 (the drain)", blocking(p))
	}
}

func TestSameTransactionAcrossOwnAccountsIsAMove(t *testing.T) {
	p := Build(cfg(nil, nil), []Movement{
		mv("e:x", 0, "ethereum:a", true, nil, d(eth, "-1")),
		mv("e:x", 0, "ethereum:b", false, nil, d(eth, "1")),
	}, nil, nil, prices)
	if e := find(t, p, "e:x"); e.Kind != Move || len(e.Legs) != 2 {
		t.Errorf("%+v", e)
	}
	if blocking(p) != 0 {
		t.Errorf("issues %v", p.Issues)
	}
}

func TestBridgesPairByTimeAndValue(t *testing.T) {
	p := Build(cfg(nil, nil), []Movement{
		mv("e:out", 0, "ethereum:me", true, []string{"0xbridge"}, d(eth, "-1")),
		mv("a:in", 20*time.Second, "arbitrum:me", false, []string{"0xrelayer"}, d(eth, "0.99")),
		mv("s:out", time.Hour, "solana:me", true, []string{"solver"}, d(sol, "-10")),
		mv("a:usd", time.Hour+30*time.Second, "arbitrum:me", false, []string{"0xsolver"}, d(usd, "1455")),
		mv("a:late", 3*time.Hour, "arbitrum:me", false, []string{"0xsomeone"}, d(usd, "1455")),
	}, nil, nil, prices)

	if e := find(t, p, "e:out+a:in"); e.Kind != Move {
		t.Errorf("same-asset bridge: %+v", e)
	}
	if e := find(t, p, "s:out+a:usd"); e.Kind != Trade {
		t.Errorf("cross-chain swap: %+v", e)
	}
	if e := find(t, p, "a:late"); e.Reason != "unresolved" {
		t.Errorf("an inflow with nothing to pair stays unresolved: %+v", e)
	}
}

func TestLabelsAndOverrides(t *testing.T) {
	c := cfg(map[string]config.Counterparty{
		"DEPOSIT": {Treat: config.TreatAccount, Account: "robinhood"},
		"HOT":     {Treat: config.TreatAccount, Account: "robinhood", Basis: "fmv"},
		"FRIEND":  {Treat: config.TreatThirdParty},
	}, map[string]config.Override{
		"s:reward": {Treat: config.OvIncome, Reason: "staking"},
		"s:nobody": {Treat: config.OvIgnore},
	})
	p := Build(c, []Movement{
		mv("s:dep", 0, "solana:me", true, []string{"DEPOSIT"}, d(sol, "-2")),
		mv("s:wd", time.Minute, "solana:me", false, []string{"HOT"}, d(sol, "2")),
		mv("s:pay", 2*time.Minute, "solana:me", true, []string{"FRIEND"}, d(sol, "-1")),
		mv("s:reward", 3*time.Minute, "solana:me", false, []string{"validator"}, d(sol, "0.5")),
	}, nil, nil, prices)

	dep := find(t, p, "s:dep")
	if dep.Kind != Move || dep.Legs[1].Account != "robinhood" || dep.Legs[1].Amount != "2" || dep.Reason != "" {
		t.Errorf("deposit: %+v", dep)
	}
	if wd := find(t, p, "s:wd"); wd.Kind != Move || wd.Legs[1].Account != "robinhood" || wd.Legs[1].Amount != "-2" || wd.Reason != "unseen_at_fmv" {
		t.Errorf("withdrawal: %+v", wd)
	}
	if e := find(t, p, "s:pay"); e.Kind != Spend {
		t.Errorf("payment: %+v", e)
	}
	if e := find(t, p, "s:reward"); e.Kind != Income || e.Reason != "staking" {
		t.Errorf("override: %+v", e)
	}
	var stale bool
	for _, i := range p.Issues {
		stale = stale || (i.Event == "s:nobody" && !i.Blocking && strings.Contains(i.Message, "matches no event"))
	}
	if !stale {
		t.Errorf("an override matching nothing must be reported: %v", p.Issues)
	}
}

func TestSameInstantInflowsComeFirst(t *testing.T) {
	p := Build(cfg(nil, nil), []Movement{
		mv("s:a-out", 0, "solana:me", true, []string{"x"}, d(sol, "-1")),
		mv("s:b-in", 0, "solana:me", false, []string{"y"}, d(sol, "1")),
	}, nil, nil, prices)
	if p.Events[0].ID != "s:b-in" {
		t.Errorf("order %v", ids(p))
	}
}

func TestFeeOnlyAndTwoSided(t *testing.T) {
	fee := mv("e:approve", 0, "ethereum:me", true, nil)
	fee.Fee = &Delta{Asset: eth, Amount: big.NewRat(1, 1000)}
	p := Build(cfg(nil, nil), []Movement{
		fee,
		mv("e:swap", time.Minute, "ethereum:me", true, nil, d(eth, "-1"), d(usd, "1990")),
	}, nil, nil, prices)
	if e := find(t, p, "e:approve"); e.Kind != Spend || e.Reason != "fee" || len(e.Fees) != 1 || *e.Fees[0].USD != "2" {
		t.Errorf("fee-only: %+v", e)
	}
	if e := find(t, p, "e:swap"); e.Kind != Trade || e.Date != "2025-03-01" {
		t.Errorf("swap: %+v", e)
	}
}
