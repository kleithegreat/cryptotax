// Package ledger turns per-account balance movements into classified tax
// events — the IR consumed by the Haskell core. Sources only report what
// changed; every classification decision lives here (docs/ledger.md).
package ledger

import (
	"math/big"
	"time"

	"github.com/kevin/cryptotax/asset"
)

// Delta is a signed change of one asset in one account.
type Delta struct {
	Asset  asset.Asset
	Amount *big.Rat
}

// Movement is the exact net effect of one source transaction on one tracked
// account. Deltas exclude the network fee, which is reported separately.
type Movement struct {
	ID      string // "<chain>:<tx>", shared by every account the tx touched
	Time    time.Time
	Account string
	Deltas  []Delta
	Fee     *Delta
	// Signed means the account holder authorized the transaction. Unsigned
	// outflows can only be spoofs (address poisoning) or theft.
	Signed bool
	// Counterparties are the external addresses on the other side.
	Counterparties []string
	Note           string
}

// Leg is an asset change in the IR. Amount is signed; USD is the fair market
// value of |Amount| at the event time, nil when no trusted price exists.
type Leg struct {
	Account string  `json:"account"`
	Asset   string  `json:"asset"`
	Symbol  string  `json:"symbol"`
	Amount  string  `json:"amount"`
	USD     *string `json:"usd"`
}

type Kind string

const (
	Trade      Kind = "trade"       // dispose negative legs, acquire positive legs, at one value
	Move       Kind = "move"        // carry lots between own accounts
	Income     Kind = "income"      // acquire at FMV, recognize ordinary income
	Spend      Kind = "spend"       // dispose at FMV (payments, fee-only transactions)
	Add        Kind = "add"         // acquire with explicit basis (spam, gifts, untracked custody)
	Remove     Kind = "remove"      // leave tracked inventory without a sale
	Perp       Kind = "perp"        // one perpetual-futures fill component
	Funding    Kind = "funding"     // perpetual funding cash flow
	BrokerSale Kind = "broker_sale" // a Form 1099-DA row
)

// Event is one entry of the IR consumed by the Haskell core.
type Event struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	// Date is the local calendar date of Time in the taxpayer's timezone; it
	// decides tax year and holding period.
	Date string `json:"date"`
	Kind Kind   `json:"kind"`
	Legs []Leg  `json:"legs"`
	Fees []Leg  `json:"fees"`
	// Reason qualifies add/remove/income (e.g. "spam", "custody", "airdrop").
	// "unresolved" marks a provisional treatment that blocks a final report.
	// On a move, "unseen_at_fmv" gives units missing from the sending account
	// a fair-market-value basis instead of blocking. On a trade, "confirmed"
	// means the taxpayer vouched for it, so lopsided values do not block.
	Reason   string      `json:"reason,omitempty"`
	Basis    *string     `json:"basis,omitempty"`    // add: total USD basis; nil = FMV
	Acquired string      `json:"acquired,omitempty"` // add: acquisition date override
	Perp     *PerpFill   `json:"perp,omitempty"`
	Funding  *FundingPay `json:"funding,omitempty"`
	Broker   *Broker     `json:"broker,omitempty"`
	Note     string      `json:"note,omitempty"`
}

// PerpFill is one perpetual-futures fill. A flip fill ("Long > Short")
// both closes the old position and opens the other side; the fee is shared
// between the parts by size, which the core does in exact arithmetic.
type PerpFill struct {
	Account string    `json:"account"`
	Market  string    `json:"market"`
	Close   *PerpPart `json:"close,omitempty"`
	Open    *PerpPart `json:"open,omitempty"`
	PnL     string    `json:"pnl"` // exchange-realized PnL of Close, gross of fees
	Fee     string    `json:"fee"` // USD, may be negative (rebate)
}

type PerpPart struct {
	Side string `json:"side"` // long | short
	Size string `json:"size"`
}

type FundingPay struct {
	Account string `json:"account"`
	Market  string `json:"market"`
	USD     string `json:"usd"` // positive received, negative paid
}

// Broker is a disposition as printed on Form 1099-DA.
type Broker struct {
	Account  string  `json:"account"`
	Asset    string  `json:"asset"`
	Symbol   string  `json:"symbol"`
	Amount   string  `json:"amount"`
	Proceeds string  `json:"proceeds"`
	Basis    *string `json:"basis"` // broker's basis; nil when the broker has none
	Term     string  `json:"term"`  // short | long | "" when the broker cannot tell
	// BasisReported is true when the broker reported basis to the IRS
	// (Form 8949 box G/J instead of H/K).
	BasisReported bool `json:"basis_reported"`
}

// Issue is a problem for the review list. Blocking issues make the report
// provisional.
type Issue struct {
	Event    string `json:"event"`
	Blocking bool   `json:"blocking"`
	Message  string `json:"message"`
}

// Payload is the full IR document.
type Payload struct {
	Version string  `json:"version"`
	Events  []Event `json:"events"`
	Issues  []Issue `json:"issues"`
	Years   []int   `json:"years"`
}

const PayloadVersion = "2"
