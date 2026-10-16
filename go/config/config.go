// Package config loads cryptotax.json: which wallets are the taxpayer's, and
// the facts no blockchain can tell us (who a counterparty is, what a transfer
// was). See cryptotax.example.json and docs/ledger.md.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Timezone  string   `json:"timezone"`
	FirstYear int      `json:"first_year"`
	Wallets   Wallets  `json:"wallets"`
	EVMChains []string `json:"evm_chains"`
	// Robinhood1099 lists consolidated 1099 CSV exports (one per tax year).
	Robinhood1099 []string `json:"robinhood_1099"`
	// Counterparties classify transfers by the external address involved.
	Counterparties map[string]Counterparty `json:"counterparties"`
	// Overrides classify individual events by id; they beat everything else.
	Overrides map[string]Override `json:"overrides"`

	Location *time.Location `json:"-"`
}

type Wallets struct {
	EVM         []string `json:"evm"`
	Solana      []string `json:"solana"`
	Hyperliquid []string `json:"hyperliquid"`
}

// Counterparty treatments.
const (
	// TreatAccount: a deposit address of a tracked account (e.g. robinhood).
	TreatAccount = "account"
	// TreatCustody: the taxpayer's own account somewhere untracked.
	TreatCustody = "custody"
	// TreatThirdParty: someone else; outflows are payments, inflows income.
	TreatThirdParty = "third_party"
	TreatSpam       = "spam"
)

type Counterparty struct {
	Treat   string `json:"treat"`
	Account string `json:"account,omitempty"` // TreatAccount: tracked account name
	// Basis "fmv" accepts fair market value at receipt as the basis of units
	// whose purchase is not in the data: every inflow from TreatCustody, and
	// units a TreatAccount account sends that were never seen arriving (e.g.
	// bought on Robinhood, then withdrawn). Empty leaves them unresolved.
	Basis string `json:"basis,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Override treatments.
const (
	OvIncome     = "income"      // Reason labels the income type
	OvSpend      = "spend"       // payment for goods/services: sale at FMV
	OvGiftOut    = "gift_out"    // no gain recognized
	OvLost       = "lost"        // stolen/lost: no deduction
	OvCustodyOut = "custody_out" // to own untracked account
	OvCustodyIn  = "custody_in"  // from own untracked account; Basis required
	OvGiftIn     = "gift_in"     // Basis and Acquired required (carryover)
	OvSpam       = "spam"
	OvIgnore     = "ignore" // not a real balance change (spoofed event)
	OvPair       = "pair"   // With names the other half of a bridge/swap
	OvTrade      = "trade"  // confirm a lopsided exchange as real
	OvBasis      = "basis"  // broker sale: supply missing basis/acquired
)

type Override struct {
	Treat    string `json:"treat"`
	Reason   string `json:"reason,omitempty"`
	Basis    string `json:"basis,omitempty"`    // USD total, or "fmv"
	Acquired string `json:"acquired,omitempty"` // YYYY-MM-DD
	With     string `json:"with,omitempty"`
	Note     string `json:"note,omitempty"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Timezone == "" {
		return nil, fmt.Errorf("%s: timezone is required (IANA name, e.g. America/New_York): tax dates are local dates", path)
	}
	if c.Location, err = time.LoadLocation(c.Timezone); err != nil {
		return nil, fmt.Errorf("%s: timezone: %w", path, err)
	}
	if c.FirstYear == 0 {
		c.FirstYear = 2023
	}
	if len(c.EVMChains) == 0 {
		c.EVMChains = []string{"ethereum", "arbitrum"}
	}
	for addr, cp := range c.Counterparties {
		switch cp.Treat {
		case TreatAccount:
			if cp.Account == "" {
				return nil, fmt.Errorf("counterparty %s: treat %q needs account", addr, cp.Treat)
			}
		case TreatCustody, TreatThirdParty, TreatSpam:
		default:
			return nil, fmt.Errorf("counterparty %s: unknown treat %q", addr, cp.Treat)
		}
		if cp.Basis != "" && cp.Basis != "fmv" {
			return nil, fmt.Errorf("counterparty %s: basis must be \"fmv\" or empty", addr)
		}
	}
	for id, ov := range c.Overrides {
		if err := ov.validate(); err != nil {
			return nil, fmt.Errorf("override %s: %w", id, err)
		}
	}
	return &c, nil
}

func (o Override) validate() error {
	switch o.Treat {
	case OvIncome, OvSpend, OvGiftOut, OvLost, OvCustodyOut, OvSpam, OvIgnore, OvTrade:
	case OvCustodyIn:
		if o.Basis == "" {
			return fmt.Errorf("custody_in needs basis (USD total or \"fmv\")")
		}
	case OvGiftIn:
		if o.Basis == "" || o.Acquired == "" {
			return fmt.Errorf("gift_in needs the donor's basis and acquisition date")
		}
	case OvPair:
		if o.With == "" {
			return fmt.Errorf("pair needs with")
		}
	case OvBasis:
		if o.Basis == "" || o.Acquired == "" {
			return fmt.Errorf("basis needs basis and acquired")
		}
	default:
		return fmt.Errorf("unknown treat %q", o.Treat)
	}
	if o.Acquired != "" {
		if _, err := time.Parse("2006-01-02", o.Acquired); err != nil {
			return fmt.Errorf("acquired: %w", err)
		}
	}
	return nil
}

// Lookup finds a counterparty label; EVM addresses compare case-insensitively.
func (c *Config) Lookup(addr string) (Counterparty, bool) {
	if cp, ok := c.Counterparties[addr]; ok {
		return cp, true
	}
	if strings.HasPrefix(addr, "0x") {
		for k, cp := range c.Counterparties {
			if strings.EqualFold(k, addr) {
				return cp, true
			}
		}
	}
	return Counterparty{}, false
}
