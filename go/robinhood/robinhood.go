// Package robinhood reads the Form 1099-DA section of Robinhood's
// consolidated 1099 CSV export. See docs/robinhood.md.
package robinhood

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/asset"
	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/ledger"
)

const Account = "robinhood"

// Read returns one broker_sale event per 1099-DA row.
func Read(path string, loc *time.Location) ([]ledger.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var cols map[string]int
	var out []ledger.Event
	for i, rec := range records {
		if len(rec) < 2 || strings.TrimSpace(rec[0]) != "1099-DA" {
			continue
		}
		if strings.TrimSpace(rec[1]) == "ACCOUNT NUMBER" {
			cols = map[string]int{}
			for j, c := range rec {
				cols[strings.TrimSpace(c)] = j
			}
			continue
		}
		if cols == nil {
			return nil, fmt.Errorf("%s line %d: 1099-DA row before its header", path, i+1)
		}
		ev, err := row(rec, cols, loc)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		out = append(out, ev)
	}
	if cols == nil {
		return nil, fmt.Errorf("%s: no 1099-DA section (wrong file or a format change)", path)
	}
	return out, nil
}

func row(rec []string, cols map[string]int, loc *time.Location) (ledger.Event, error) {
	get := func(name string) string {
		if j, ok := cols[name]; ok && j < len(rec) {
			return strings.TrimSpace(rec[j])
		}
		return ""
	}
	year := get("TAX YEAR")
	a, ok := asset.ByDTI(get("DTIF CODE"))
	if !ok {
		return ledger.Event{}, fmt.Errorf("unknown DTI %q (%s): add it to asset.dtis from a verified source", get("DTIF CODE"), get("DTIF NAME"))
	}
	units, err := decimal.Canon(get("DTIF UNITS"))
	if err != nil {
		return ledger.Event{}, fmt.Errorf("DTIF UNITS: %w", err)
	}
	proceeds, err := decimal.Canon(get("SALES PRICE"))
	if err != nil {
		return ledger.Event{}, fmt.Errorf("SALES PRICE: %w", err)
	}

	date := get("SALE DATE")
	if date == "" {
		// Aggregated stablecoin rows carry no sale date; for cash the date
		// only selects the tax year.
		if !asset.IsCash(a.Key) {
			return ledger.Event{}, fmt.Errorf("row without SALE DATE")
		}
		date = year + "1231"
	}
	day, err := time.ParseInLocation("20060102", date, loc)
	if err != nil {
		return ledger.Event{}, fmt.Errorf("SALE DATE %q: %w", date, err)
	}

	b := &ledger.Broker{
		Account:       Account,
		Asset:         a.Key,
		Symbol:        a.Symbol,
		Amount:        units,
		Proceeds:      proceeds,
		BasisReported: get("FORM8949CODE") == "G" || get("FORM8949CODE") == "J",
	}
	switch get("TERM") {
	case "SHORT":
		b.Term = "short"
	case "LONG":
		b.Term = "long"
	}
	// The broker knows a basis only when it states both basis and term; a
	// "0" basis on transferred-in units (code X) means "unknown", not zero.
	if basis := get("COST BASIS"); basis != "" && b.Term != "" && get("FORM8949CODE") != "X" {
		c, err := decimal.Canon(basis)
		if err != nil {
			return ledger.Event{}, fmt.Errorf("COST BASIS: %w", err)
		}
		b.Basis = &c
	}

	note := get("DESCRIPTION")
	if in := get("DA NUM UNITS TRANS IN"); in != "" {
		note += fmt.Sprintf("; broker reports %s units transferred in on %s", in, get("DA TRANSFER IN DATE"))
	}
	if get("AGGREGATE TRANS IND") == "Y" || get("AGG NUM TRANS") != "" {
		note += "; aggregated by broker"
	}
	return ledger.Event{
		// The broker's own row identity: same account, year, asset, date,
		// units and proceeds never describe two different sales.
		ID:     fmt.Sprintf("robinhood:%s:%s:%s:%s:%s", year, date, a.Key, units, proceeds),
		Time:   day.Add(24*time.Hour - time.Second),
		Kind:   ledger.BrokerSale,
		Broker: b,
		Note:   note,
	}, nil
}
