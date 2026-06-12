package fetcher

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/types"
)

// Robinhood parses the consolidated 1099 CSV export from Robinhood.
// The CSV contains sections: 1099-DIV, 1099-INT, 1099-B, 1099-DA, 1099-MISC.
// We extract only 1099-DA (Digital Asset) rows for actual crypto transactions.
// 1099-B rows may contain crypto-related ETFs but those are equity, not crypto.
type Robinhood struct {
	FilePath string
}

func NewRobinhood(filePath string) *Robinhood {
	return &Robinhood{FilePath: filePath}
}

func (r *Robinhood) Name() string { return "robinhood" }

// cryptoNames maps full crypto names to ticker symbols. Matching is exact
// (whole name, or name plus its own trailing ticker) — substring matching
// would misidentify e.g. "Bitcoin Cash" as BTC or "Ethereum Classic" as ETH.
var cryptoNames = map[string]string{
	"BITCOIN":   "BTC",
	"ETHEREUM":  "ETH",
	"SOLANA":    "SOL",
	"DOGECOIN":  "DOGE",
	"AVALANCHE": "AVAX",
	"CARDANO":   "ADA",
	"CHAINLINK": "LINK",
	"UNISWAP":   "UNI",
	"AAVE":      "AAVE",
	"POLYGON":   "MATIC",
	"SHIBA INU": "SHIB",
	"XRP":       "XRP",
	"USDC":      "USDC",
	"USD COIN":  "USDC",
	"USDT":      "USDT",
	"TETHER":    "USDT",
}

// cryptoSymbols is the set of known tickers (values of cryptoNames).
var cryptoSymbols = func() map[string]bool {
	set := make(map[string]bool, len(cryptoNames))
	for _, sym := range cryptoNames {
		set[sym] = true
	}
	return set
}()

func (r *Robinhood) Fetch(_ string) ([]RawTransaction, error) {
	f, err := os.Open(r.FilePath)
	if err != nil {
		return nil, fmt.Errorf("opening CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // allow variable column counts across sections

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}

	return r.parse1099DA(records)
}

// parse1099DA extracts crypto transactions from the 1099-DA section.
// The 1099-DA header row defines columns:
// ACCOUNT NUMBER, TAX YEAR, DATE ACQUIRED, SALE DATE, DESCRIPTION,
// DTIF CODE, DTIF NAME, DTIF UNITS, COST BASIS, SALES PRICE, TERM, ...
func (r *Robinhood) parse1099DA(records [][]string) ([]RawTransaction, error) {
	// Find the 1099-DA header row
	var colIdx map[string]int
	var txs []RawTransaction

	inSection := false
	sectionSeen := false
	for rowNum, row := range records {
		if len(row) == 0 {
			continue
		}

		recordType := strings.TrimSpace(row[0])

		// Detect section header: first column is "1099-DA" and second is "ACCOUNT NUMBER"
		if recordType == "1099-DA" && len(row) > 1 && strings.TrimSpace(row[1]) == "ACCOUNT NUMBER" {
			colIdx = make(map[string]int)
			for i, col := range row[1:] { // skip the record type prefix
				colIdx[strings.TrimSpace(col)] = i + 1
			}
			inSection = true
			sectionSeen = true
			continue
		}

		// Data rows in the 1099-DA section
		if recordType == "1099-DA" && inSection && colIdx != nil {
			rowTxs := r.parseDARow(rowNum+1, row, colIdx)
			txs = append(txs, rowTxs...)
		}

		// Different section started — stop processing 1099-DA
		if recordType != "1099-DA" && inSection {
			inSection = false
		}
	}

	// A consolidated 1099 without the expected section means header drift or
	// the wrong file; returning zero rows silently would hide all activity.
	if !sectionSeen {
		return nil, fmt.Errorf("no 1099-DA section header found in %s — wrong file or unsupported export format", r.FilePath)
	}

	return txs, nil
}

// parseDARow converts one 1099-DA row into up to two synthetic raw rows
// (acquisition and disposition). Rows are never silently dropped: assets
// with unrecognized DTIF names keep the verbatim name as their asset string
// (flagged downstream by the suspicious-asset audit), and malformed dates
// skip only the affected leg with a stderr warning so the remaining leg
// still surfaces loudly in the core.
func (r *Robinhood) parseDARow(rowNum int, row []string, colIdx map[string]int) []RawTransaction {
	getCol := func(name string) string {
		idx, ok := colIdx[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	dtifName := getCol("DTIF NAME")
	units := cleanRobinhoodDecimal(getCol("DTIF UNITS"))
	costBasis := cleanRobinhoodDecimal(getCol("COST BASIS"))
	salesPrice := cleanRobinhoodDecimal(getCol("SALES PRICE"))
	saleDate := getCol("SALE DATE")
	acquiredDate := getCol("DATE ACQUIRED")
	term := getCol("TERM")

	asset := resolveCryptoSymbol(dtifName)
	if asset == "" {
		if strings.TrimSpace(dtifName) == "" {
			fmt.Fprintf(os.Stderr, "Warning: 1099-DA row %d has no DTIF NAME; skipping\n", rowNum)
			return nil
		}
		// Preserve the source-backed name instead of guessing a ticker.
		asset = strings.TrimSpace(dtifName)
	}

	unitsSign, err := decimal.Sign(units)
	if err != nil || unitsSign <= 0 {
		if units != "" && units != "0" {
			fmt.Fprintf(os.Stderr, "Warning: 1099-DA row %d has unusable DTIF UNITS %q; skipping\n", rowNum, units)
		}
		return nil
	}

	var txs []RawTransaction
	groupID := fmt.Sprintf("robinhood:1099da:r%d:%s:%s:%s:%s", rowNum, acquiredDate, saleDate, asset, units)

	// Synthetic buy (acquisition): requires a parseable date and a reported
	// basis. A basis reported as exactly "0" is source data, not a guess.
	if acquiredDate != "" && costBasis != "" {
		acquiredTS, err := parseRobinhoodDate(acquiredDate)
		if err != nil {
			// e.g. "VARIOUS" — basis is known but the acquisition date is
			// not, which FIFO cannot represent without inventing a holding
			// period. The disposition below still surfaces in the core.
			fmt.Fprintf(os.Stderr,
				"Warning: 1099-DA row %d DATE ACQUIRED %q unsupported; emitting disposition without acquisition (basis must be resolved manually)\n",
				rowNum, acquiredDate)
		} else {
			txs = append(txs, RawTransaction{
				ID:           fmt.Sprintf("rh-buy-r%d-%s-%s-%s", rowNum, acquiredDate, asset, units),
				Timestamp:    acquiredTS,
				Source:       types.SourceRobinhood,
				Chain:        types.ChainRobinhood,
				Wallet:       "robinhood",
				EventGroupID: groupID,
				SplitReason:  "synthetic_1099da_row",
				Asset:        asset,
				Amount:       units,
				USDPrice:     costBasis, // total lot cost basis from Robinhood, not per-unit
				RawType:      "1099-DA-BUY",
			})
		}
	}

	// Create a sell (disposition) if we have a sale date and proceeds
	if saleDate != "" && salesPrice != "" {
		saleTS, err := parseRobinhoodDate(saleDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: 1099-DA row %d SALE DATE %q unsupported; skipping disposition\n", rowNum, saleDate)
		} else {
			rawType := "1099-DA-SELL"
			if term != "" {
				rawType = fmt.Sprintf("1099-DA-SELL-%s", term)
			}
			txs = append(txs, RawTransaction{
				ID:           fmt.Sprintf("rh-sell-r%d-%s-%s-%s", rowNum, saleDate, asset, units),
				Timestamp:    saleTS,
				Source:       types.SourceRobinhood,
				Chain:        types.ChainRobinhood,
				Wallet:       "robinhood",
				EventGroupID: groupID,
				SplitReason:  "synthetic_1099da_row",
				Asset:        asset,
				Amount:       units,
				USDPrice:     salesPrice, // total sale proceeds from Robinhood, not per-unit
				RawType:      rawType,
			})
		}
	}

	if len(txs) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: 1099-DA row %d produced no usable legs (asset %q)\n", rowNum, asset)
	}
	return txs
}

// resolveCryptoSymbol extracts a crypto ticker from a DTIF name, or returns
// "" when the name is not positively identified. Matching is deterministic
// and exact: the whole name is a known name or ticker, or the name's last
// word is a known ticker (e.g. "Solana SOL" → SOL). Unknown names are NOT
// guessed; callers preserve them verbatim so they stay visible downstream.
func resolveCryptoSymbol(dtifName string) string {
	fields := strings.Fields(strings.ToUpper(dtifName))
	if len(fields) == 0 {
		return ""
	}
	full := strings.Join(fields, " ")

	if sym, ok := cryptoNames[full]; ok {
		return sym
	}
	if cryptoSymbols[full] {
		return full
	}

	last := fields[len(fields)-1]
	if !cryptoSymbols[last] {
		return ""
	}
	// The last word is a known ticker. Accept it only when the leading name
	// is absent or agrees with it ("Solana SOL"), so a hypothetical
	// "Wrapped SOL" style listing cannot silently collapse into SOL.
	leading := strings.Join(fields[:len(fields)-1], " ")
	if leading == "" || cryptoNames[leading] == last {
		return last
	}
	return ""
}

func parseRobinhoodDate(value string) (int64, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		"01/02/2006",
		"1/2/2006",
		"2006-01-02",
		"20060102",
	}

	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err == nil {
			return parsed.Unix(), nil
		}
	}

	return 0, fmt.Errorf("unsupported date format")
}

func cleanRobinhoodDecimal(value string) string {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")")
	value = strings.TrimPrefix(value, "(")
	value = strings.TrimSuffix(value, ")")
	value = strings.TrimPrefix(value, "$")
	value = strings.ReplaceAll(value, ",", "")
	if negative && value != "" {
		return "-" + value
	}
	return value
}
