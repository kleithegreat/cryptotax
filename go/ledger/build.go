package ledger

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/kevin/cryptotax/config"
	"github.com/kevin/cryptotax/decimal"
)

// Pricer values |amount| units of an asset at t; ok is false when no trusted
// price exists.
type Pricer interface {
	Value(key string, amount *big.Rat, t time.Time) (*big.Rat, bool)
}

var (
	// Inflows worth less than this that the holder did not sign are dust
	// (address poisoning, airdrop spam), not income.
	dustUSD = big.NewRat(1, 100)
	// Bridge pairing: the inflow must land within this window of the outflow
	// (chains disagree on clocks by a little) and be worth 90%–102% of it.
	pairBefore   = 2 * time.Minute
	pairAfter    = 30 * time.Minute
	pairMinRatio = big.NewRat(90, 100)
	pairMaxRatio = big.NewRat(102, 100)
)

type builder struct {
	cfg    *config.Config
	price  Pricer
	out    []Event
	issues []Issue
	used   map[string]bool // overrides consumed
}

// valued is a movement with its legs priced.
type valued struct {
	m    Movement
	legs []Leg
	fees []Leg
	usd  *big.Rat // total |value| of deltas; nil if any delta is unpriced
	// net is value out minus value in, for pairing a movement whose
	// counterpart lands in another account; nil if any delta is unpriced.
	net *big.Rat
}

func (b *builder) value(m Movement) valued {
	v := valued{m: m, usd: new(big.Rat), net: new(big.Rat)}
	for _, d := range m.Deltas {
		l := Leg{Account: m.Account, Asset: d.Asset.Key, Symbol: d.Asset.Symbol, Amount: decimal.String(d.Amount)}
		if u, ok := b.price.Value(d.Asset.Key, d.Amount, m.Time); ok {
			s := decimal.String(u)
			l.USD = &s
			if v.usd != nil {
				v.usd.Add(v.usd, u)
				if d.Amount.Sign() < 0 {
					v.net.Add(v.net, u)
				} else {
					v.net.Sub(v.net, u)
				}
			}
		} else {
			v.usd, v.net = nil, nil
		}
		v.legs = append(v.legs, l)
	}
	if m.Fee != nil {
		l := Leg{Account: m.Account, Asset: m.Fee.Asset.Key, Symbol: m.Fee.Asset.Symbol, Amount: decimal.String(m.Fee.Amount)}
		if u, ok := b.price.Value(m.Fee.Asset.Key, m.Fee.Amount, m.Time); ok {
			s := decimal.String(u)
			l.USD = &s
		}
		v.fees = append(v.fees, l)
	}
	return v
}

func (v valued) sign() int {
	pos, neg := false, false
	for _, d := range v.m.Deltas {
		if d.Amount.Sign() > 0 {
			pos = true
		} else {
			neg = true
		}
	}
	switch {
	case pos && neg:
		return 0
	case pos:
		return 1
	case neg:
		return -1
	}
	return 2 // fee only
}

func (b *builder) emit(e Event) { b.out = append(b.out, e) }

func (b *builder) issue(id string, blocking bool, format string, args ...any) {
	b.issues = append(b.issues, Issue{Event: id, Blocking: blocking, Message: fmt.Sprintf(format, args...)})
}

func event(kind Kind, vs ...valued) Event {
	e := Event{ID: vs[0].m.ID, Time: vs[0].m.Time, Kind: kind}
	var notes []string
	for _, v := range vs {
		e.Legs = append(e.Legs, v.legs...)
		e.Fees = append(e.Fees, v.fees...)
		if v.m.Note != "" {
			notes = append(notes, v.m.Note)
		}
		if v.m.Time.Before(e.Time) {
			e.Time = v.m.Time
		}
	}
	if len(vs) > 1 {
		var ids []string
		for _, v := range vs {
			if v.m.ID != e.ID {
				ids = append(ids, v.m.ID)
			}
		}
		if len(ids) > 0 {
			e.ID += "+" + strings.Join(ids, "+")
		}
	}
	e.Note = strings.Join(notes, "; ")
	return e
}

// Build classifies movements into events. direct holds events sources could
// already classify (perps, funding, broker rows).
func Build(cfg *config.Config, ms []Movement, direct []Event, issues []Issue, price Pricer) Payload {
	b := &builder{cfg: cfg, price: price, issues: issues, used: map[string]bool{}}

	byID := map[string][]valued{}
	var ids []string
	for _, m := range ms {
		if _, ok := byID[m.ID]; !ok {
			ids = append(ids, m.ID)
		}
		byID[m.ID] = append(byID[m.ID], b.value(m))
	}
	sort.Slice(ids, func(i, j int) bool {
		ti, tj := byID[ids[i]][0].m.Time, byID[ids[j]][0].m.Time
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return ids[i] < ids[j]
	})

	// Explicit pairings first: they consume both halves.
	done := map[string]bool{}
	for _, id := range ids {
		ov, ok := cfg.Overrides[id]
		if !ok || ov.Treat != config.OvPair || done[id] {
			continue
		}
		b.used[id] = true
		other, ok := byID[ov.With]
		if !ok {
			b.issue(id, true, "override pairs with %s, which does not exist", ov.With)
			continue
		}
		done[id], done[ov.With] = true, true
		if _, ok := cfg.Overrides[ov.With]; ok {
			b.used[ov.With] = true
		}
		b.combine(append(append([]valued{}, byID[id]...), other...), "confirmed")
	}

	var outs, ins, trades []valued
	for _, id := range ids {
		if done[id] {
			continue
		}
		group := byID[id]
		if ov, ok := cfg.Overrides[id]; ok {
			b.used[id] = true
			for _, v := range group {
				b.override(v, ov)
			}
			continue
		}
		if len(group) > 1 {
			b.combine(group, "")
			continue
		}
		v := group[0]
		switch v.sign() {
		case 2:
			e := event(Spend, v)
			e.Reason = "fee"
			b.emit(e)
		case 0:
			trades = append(trades, v)
		case 1:
			if !b.labeled(v) && !b.spamIn(v) {
				ins = append(ins, v)
			}
		case -1:
			if !b.labeled(v) && !b.spoofOut(v) {
				outs = append(outs, v)
			}
		}
	}

	// A swap executed by a cross-chain solver often refunds dust on the
	// source chain, so it looks two-sided; it may still pair by net value.
	left := b.pair(append(outs, trades...), ins)
	for _, v := range left {
		if v.sign() == 0 {
			b.emit(event(Trade, v))
			continue
		}
		e := event(Remove, v)
		if v.sign() > 0 {
			e = event(Add, v)
		}
		e.Reason = "unresolved"
		b.emit(e)
		dir := "outflow to"
		if v.sign() > 0 {
			dir = "inflow from"
		}
		b.issue(v.m.ID, true, "unclassified %s %s worth %s: label the counterparty or override this id", dir, cps(v.m), usdText(v.usd))
	}

	for _, e := range direct {
		if e.Kind == BrokerSale {
			if ov, ok := cfg.Overrides[e.ID]; ok {
				b.used[e.ID] = true
				if ov.Treat != config.OvBasis {
					b.issue(e.ID, true, "broker sales only accept the %q override", config.OvBasis)
				} else {
					basis := ov.Basis
					e.Broker.Basis, e.Acquired = &basis, ov.Acquired
				}
			}
		}
		b.emit(e)
	}

	for id := range cfg.Overrides {
		if !b.used[id] {
			b.issue(id, false, "override matches no event")
		}
	}

	for i := range b.out {
		b.out[i].Date = b.out[i].Time.In(cfg.Location).Format("2006-01-02")
		if b.out[i].Legs == nil {
			b.out[i].Legs = []Leg{}
		}
		if b.out[i].Fees == nil {
			b.out[i].Fees = []Leg{}
		}
	}
	// Sources timestamp to the second (Solana gives no intra-block order),
	// and a balance never goes negative: within one instant, what only
	// arrives is applied before what also leaves.
	sort.SliceStable(b.out, func(i, j int) bool {
		if !b.out[i].Time.Equal(b.out[j].Time) {
			return b.out[i].Time.Before(b.out[j].Time)
		}
		if ri, rj := rank(b.out[i]), rank(b.out[j]); ri != rj {
			return ri < rj
		}
		return b.out[i].ID < b.out[j].ID
	})

	now := time.Now().In(cfg.Location).Year()
	var years []int
	for y := cfg.FirstYear; y <= now; y++ {
		years = append(years, y)
	}
	if b.issues == nil {
		b.issues = []Issue{}
	}
	return Payload{Version: PayloadVersion, Events: b.out, Issues: b.issues, Years: years}
}

func rank(e Event) int {
	arrives, leaves := false, len(e.Fees) > 0
	for _, l := range e.Legs {
		if strings.HasPrefix(l.Amount, "-") {
			leaves = true
		} else {
			arrives = true
		}
	}
	switch {
	case arrives && !leaves:
		return 0
	case arrives && leaves:
		return 1
	}
	return 2
}

// combine merges movements of one economic event across own accounts: a
// single asset moving between accounts is a move; different assets out and
// in are a trade (e.g. a swap whose output lands in another own wallet).
func (b *builder) combine(vs []valued, reason string) {
	outKeys, inKeys := map[string]bool{}, map[string]bool{}
	for _, v := range vs {
		for _, d := range v.m.Deltas {
			if d.Amount.Sign() < 0 {
				outKeys[d.Asset.Key] = true
			} else {
				inKeys[d.Asset.Key] = true
			}
		}
	}
	switch {
	case len(outKeys) == 1 && len(inKeys) == 1 && sameKeys(outKeys, inKeys):
		b.emit(event(Move, vs...))
	case len(outKeys) > 0 && len(inKeys) > 0 && disjoint(outKeys, inKeys):
		e := event(Trade, vs...)
		e.Reason = reason
		b.emit(e)
	case len(outKeys) == 0 && len(inKeys) == 0:
		e := event(Spend, vs...)
		e.Reason = "fee"
		b.emit(e)
	default:
		e := event(Remove, vs...)
		e.Reason = "unresolved"
		b.emit(e)
		b.issue(e.ID, true, "transaction touching several own accounts has a shape that is neither a move nor a swap")
	}
}

// labeled applies a counterparty label to a one-sided movement.
func (b *builder) labeled(v valued) bool {
	var cp config.Counterparty
	found := false
	for _, c := range v.m.Counterparties {
		if cp, found = b.cfg.Lookup(c); found {
			break
		}
	}
	if !found {
		return false
	}
	in := v.sign() > 0
	switch cp.Treat {
	case config.TreatAccount:
		// Synthesize the tracked account's side of the transfer.
		mirror := v
		mirror.legs = nil
		mirror.fees = nil
		for _, l := range v.legs {
			l.Account = cp.Account
			l.Amount = negate(l.Amount)
			mirror.legs = append(mirror.legs, l)
		}
		mirror.m.Note = ""
		e := event(Move, v, mirror)
		if cp.Basis == "fmv" {
			e.Reason = "unseen_at_fmv"
		}
		b.emit(e)
	case config.TreatCustody:
		if in {
			e := event(Add, v)
			e.Reason = "custody"
			if cp.Basis != "fmv" {
				e.Reason = "unresolved"
				b.issue(v.m.ID, true, "inflow from own untracked account (%s): its cost basis is unknown; override with custody_in", cp.Note)
			}
			b.emit(e)
		} else {
			e := event(Remove, v)
			e.Reason = "custody"
			b.emit(e)
		}
	case config.TreatThirdParty:
		if in {
			e := event(Income, v)
			e.Reason = "received"
			b.emit(e)
		} else {
			b.emit(event(Spend, v))
		}
	case config.TreatSpam:
		if in {
			e := event(Add, v)
			e.Reason = "spam"
			zero := "0"
			e.Basis = &zero
			b.emit(e)
		}
	}
	return true
}

func (b *builder) spamIn(v valued) bool {
	if v.m.Signed || (v.usd != nil && v.usd.Cmp(dustUSD) >= 0) {
		return false
	}
	e := event(Add, v)
	e.Reason = "spam"
	zero := "0"
	e.Basis = &zero
	b.emit(e)
	return true
}

// spoofOut drops outflows the holder never signed whose assets have no
// trusted value: address-poisoning contracts emit fake Transfer events
// "from" the victim for tokens the victim never held.
func (b *builder) spoofOut(v valued) bool {
	if v.m.Signed || (v.usd != nil && v.usd.Cmp(dustUSD) >= 0) {
		return false
	}
	b.issue(v.m.ID, false, "ignored unsigned outflow of worthless asset (address poisoning)")
	return true
}

func (b *builder) override(v valued, ov config.Override) {
	s := v.sign()
	wantIn := map[string]bool{config.OvIncome: true, config.OvCustodyIn: true, config.OvGiftIn: true, config.OvSpam: true}[ov.Treat]
	wantOut := map[string]bool{config.OvSpend: true, config.OvGiftOut: true, config.OvLost: true, config.OvCustodyOut: true}[ov.Treat]
	if (wantIn && s != 1) || (wantOut && s != -1) || (ov.Treat == config.OvTrade && s != 0) || ov.Treat == config.OvBasis {
		b.issue(v.m.ID, true, "override %q does not fit this movement", ov.Treat)
		b.emit(event(Trade, v))
		return
	}
	var e Event
	switch ov.Treat {
	case config.OvIgnore:
		return
	case config.OvTrade:
		e = event(Trade, v)
		e.Reason = "confirmed"
	case config.OvIncome:
		e = event(Income, v)
		e.Reason = ov.Reason
		if e.Reason == "" {
			e.Reason = "income"
		}
	case config.OvSpend:
		e = event(Spend, v)
	case config.OvGiftOut, config.OvLost, config.OvCustodyOut:
		e = event(Remove, v)
		e.Reason = strings.TrimSuffix(ov.Treat, "_out")
	case config.OvSpam:
		e = event(Add, v)
		e.Reason = "spam"
		zero := "0"
		e.Basis = &zero
	case config.OvCustodyIn, config.OvGiftIn:
		e = event(Add, v)
		e.Reason = strings.TrimSuffix(ov.Treat, "_in")
		if ov.Basis != "fmv" {
			basis := ov.Basis
			e.Basis = &basis
		}
		e.Acquired = ov.Acquired
	}
	e.Note = strings.TrimSpace(e.Note + " " + ov.Note)
	b.emit(e)
}

// pair matches unclassified outflows with unclassified inflows of another own
// account that land shortly after with about the same value: bridges and
// cross-chain swaps executed by solvers. It returns what stayed unmatched.
func (b *builder) pair(outs, ins []valued) []valued {
	taken := map[int]bool{}
	var left []valued
	for _, o := range outs {
		best := -1
		var bestGap time.Duration
		for i, in := range ins {
			if taken[i] || in.m.Account == o.m.Account || len(in.m.Deltas) != 1 || (o.sign() < 0 && len(o.m.Deltas) != 1) {
				continue
			}
			gap := in.m.Time.Sub(o.m.Time)
			if gap < -pairBefore || gap > pairAfter || !comparable(o, in) {
				continue
			}
			if best < 0 || absDur(gap) < bestGap {
				best, bestGap = i, absDur(gap)
			}
		}
		if best < 0 {
			left = append(left, o)
			continue
		}
		taken[best] = true
		in := ins[best]
		kind := Trade
		if o.sign() < 0 && o.m.Deltas[0].Asset.Key == in.m.Deltas[0].Asset.Key {
			kind = Move
		}
		e := event(kind, o, in)
		b.emit(e)
		b.issue(e.ID, false, "auto-paired %s → %s as a bridge/cross-chain %s (%s apart)", o.m.Account, in.m.Account, kind, bestGap)
	}
	for i, in := range ins {
		if !taken[i] {
			left = append(left, in)
		}
	}
	return left
}

// comparable: the inflow is worth 90%–102% of the outflow's net value, or of
// its quantity for the same asset when it has no price.
func comparable(o, in valued) bool {
	var ov, iv *big.Rat
	switch {
	case o.net != nil && in.usd != nil:
		ov, iv = o.net, in.usd
	case o.sign() < 0 && o.m.Deltas[0].Asset.Key == in.m.Deltas[0].Asset.Key:
		ov = new(big.Rat).Abs(o.m.Deltas[0].Amount)
		iv = new(big.Rat).Abs(in.m.Deltas[0].Amount)
	default:
		return false
	}
	if ov.Sign() <= 0 {
		return false
	}
	r := new(big.Rat).Quo(iv, ov)
	return r.Cmp(pairMinRatio) >= 0 && r.Cmp(pairMaxRatio) <= 0
}

func sameKeys(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func disjoint(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return false
		}
	}
	return true
}

func negate(s string) string {
	if strings.HasPrefix(s, "-") {
		return s[1:]
	}
	return "-" + s
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func cps(m Movement) string {
	if len(m.Counterparties) == 0 {
		return "(no counterparty)"
	}
	return strings.Join(m.Counterparties, ",")
}

func usdText(v *big.Rat) string {
	if v == nil {
		return "an unknown amount (unpriced)"
	}
	return "$" + v.FloatString(2)
}
