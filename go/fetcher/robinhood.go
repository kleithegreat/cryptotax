package fetcher

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

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

// cryptoNames maps common crypto descriptions to ticker symbols.
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
	"USDT":      "USDT",
}

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
			continue
		}

		// Data rows in the 1099-DA section
		if recordType == "1099-DA" && inSection && colIdx != nil {
			tx, ok, err := r.parseDARow(row, colIdx)
			if err != nil {
				return nil, fmt.Errorf("1099-DA row %d: %w", rowNum+1, err)
			}
			if ok {
				txs = append(txs, tx...)
			}
		}

		// Different section started — stop processing 1099-DA
		if recordType != "1099-DA" && inSection {
			inSection = false
		}
	}

	return txs, nil
}

func (r *Robinhood) parseDARow(row []string, colIdx map[string]int) ([]RawTransaction, bool, error) {
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

	// Resolve crypto symbol from DTIF NAME
	symbol := resolveCryptoSymbol(dtifName)
	if symbol == "" {
		return nil, false, nil
	}

	// Skip rows with no units
	if units == "" || units == "0" {
		return nil, false, nil
	}

	var txs []RawTransaction
	groupID := robinhoodRowGroupID(acquiredDate, saleDate, symbol, units)

	// Create a synthetic buy (acquisition) if we have a cost basis
	if acquiredDate != "" && costBasis != "" && costBasis != "0" {
		acquiredTS, err := parseRobinhoodDate(acquiredDate)
		if err != nil {
			return nil, false, fmt.Errorf("invalid DATE ACQUIRED %q: %w", acquiredDate, err)
		}
		txs = append(txs, RawTransaction{
			ID:           fmt.Sprintf("rh-buy-%s-%s-%s", acquiredDate, symbol, units),
			Timestamp:    acquiredTS,
			Source:       types.SourceRobinhood,
			Chain:        types.ChainRobinhood,
			Wallet:       "robinhood",
			EventGroupID: groupID,
			SplitReason:  "synthetic_1099da_row",
			Asset:        symbol,
			Amount:       units,
			USDPrice:     costBasis, // total lot cost basis from Robinhood, not per-unit
			RawType:      "1099-DA-BUY",
		})
	}

	// Create a sell (disposition) if we have a sale date and proceeds
	if saleDate != "" && salesPrice != "" {
		saleTS, err := parseRobinhoodDate(saleDate)
		if err != nil {
			return nil, false, fmt.Errorf("invalid SALE DATE %q: %w", saleDate, err)
		}
		rawType := "1099-DA-SELL"
		if term != "" {
			rawType = fmt.Sprintf("1099-DA-SELL-%s", term)
		}
		txs = append(txs, RawTransaction{
			ID:           fmt.Sprintf("rh-sell-%s-%s-%s", saleDate, symbol, units),
			Timestamp:    saleTS,
			Source:       types.SourceRobinhood,
			Chain:        types.ChainRobinhood,
			Wallet:       "robinhood",
			EventGroupID: groupID,
			SplitReason:  "synthetic_1099da_row",
			Asset:        symbol,
			Amount:       units,
			USDPrice:     salesPrice, // total sale proceeds from Robinhood, not per-unit
			RawType:      rawType,
		})
	}

	return txs, len(txs) > 0, nil
}

func robinhoodRowGroupID(acquiredDate, saleDate, symbol, units string) string {
	return fmt.Sprintf("robinhood:1099da:%s:%s:%s:%s", acquiredDate, saleDate, symbol, units)
}

// resolveCryptoSymbol extracts a crypto ticker from a DTIF name.
// Returns empty string for non-crypto assets.
func resolveCryptoSymbol(dtifName string) string {
	upper := strings.ToUpper(dtifName)

	// Direct match against the name portion (e.g., "Solana SOL" → "SOL")
	for name, sym := range cryptoNames {
		if strings.Contains(upper, name) {
			return sym
		}
	}

	// Check if the name itself is a known symbol (e.g., "USDC")
	if _, ok := cryptoNames[upper]; ok {
		return cryptoNames[upper]
	}

	// Check if the last word is a known ticker (e.g., "Solana SOL" → "SOL")
	parts := strings.Fields(dtifName)
	if len(parts) > 0 {
		lastWord := strings.ToUpper(parts[len(parts)-1])
		for _, sym := range cryptoNames {
			if lastWord == sym {
				return sym
			}
		}
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
