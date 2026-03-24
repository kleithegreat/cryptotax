package audit

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/kevin/cryptotax/types"
)

type Filters struct {
	TxID          string
	FromTimestamp *time.Time
	ToTimestamp   *time.Time
	Wallet        string
	Asset         string
	RawType       string
}

type Summary struct {
	Version             string                  `json:"version"`
	Wallets             []string                `json:"wallets,omitempty"`
	TransactionCount    int                     `json:"transaction_count"`
	UniqueIDCount       int                     `json:"unique_id_count"`
	MissingRawTypeCount int                     `json:"missing_raw_type_count,omitempty"`
	TimestampRange      *TimestampRange         `json:"timestamp_range,omitempty"`
	BySource            map[types.Source]int    `json:"by_source,omitempty"`
	ByChain             map[types.Chain]int     `json:"by_chain,omitempty"`
	ByTxType            map[types.TxType]int    `json:"by_tx_type,omitempty"`
	ByWallet            map[string]int          `json:"by_wallet,omitempty"`
	ByRawType           map[string]int          `json:"by_raw_type,omitempty"`
	ByAsset             map[string]AssetSummary `json:"by_asset,omitempty"`
}

type TimestampRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type AssetSummary struct {
	TransactionCount int    `json:"transaction_count"`
	SentCount        int    `json:"sent_count,omitempty"`
	SentAmount       string `json:"sent_amount"`
	ReceivedCount    int    `json:"received_count,omitempty"`
	ReceivedAmount   string `json:"received_amount"`
	FeeCount         int    `json:"fee_count,omitempty"`
	FeeAmount        string `json:"fee_amount"`
}

func ParseTimestamp(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}

	utc := timestamp.UTC()
	return &utc, nil
}

func LoadPayload(path string) (types.TxPayload, error) {
	payloadJSON, err := os.ReadFile(path)
	if err != nil {
		return types.TxPayload{}, fmt.Errorf("read payload %s: %w", path, err)
	}

	var payload types.TxPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return types.TxPayload{}, fmt.Errorf("unmarshal payload %s: %w", path, err)
	}

	return payload, nil
}

func WritePayload(path string, payload types.TxPayload) error {
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, append(payloadJSON, '\n'), 0o644); err != nil {
		return fmt.Errorf("write payload %s: %w", path, err)
	}

	return nil
}

func WriteCaptureArtifacts(outputPath string, payload types.TxPayload, commandLine string) error {
	if err := WritePayload(outputPath, payload); err != nil {
		return err
	}

	commandContents := fmt.Sprintf("# Re-run command\n%s\n", strings.TrimSpace(commandLine))
	commandPath := outputPath + ".command"
	if err := os.WriteFile(commandPath, []byte(commandContents), 0o644); err != nil {
		return fmt.Errorf("write command file %s: %w", commandPath, err)
	}

	return nil
}

func FilterPayload(payload types.TxPayload, filters Filters) types.TxPayload {
	filtered := types.TxPayload{
		Version: payload.Version,
		Wallets: append([]string(nil), payload.Wallets...),
	}

	for _, tx := range payload.Transactions {
		if matchesFilters(tx, filters) {
			filtered.Transactions = append(filtered.Transactions, tx)
		}
	}

	return filtered
}

func BuildSummary(payload types.TxPayload) (Summary, error) {
	summary := Summary{
		Version:   payload.Version,
		Wallets:   append([]string(nil), payload.Wallets...),
		BySource:  make(map[types.Source]int),
		ByChain:   make(map[types.Chain]int),
		ByTxType:  make(map[types.TxType]int),
		ByWallet:  make(map[string]int),
		ByRawType: make(map[string]int),
		ByAsset:   make(map[string]AssetSummary),
	}

	uniqueIDs := make(map[string]struct{})
	assetAccumulators := make(map[string]*assetSummaryAccumulator)

	for index, tx := range payload.Transactions {
		summary.TransactionCount++
		summary.BySource[tx.Source]++
		summary.ByChain[tx.Chain]++
		summary.ByTxType[tx.TxType]++
		summary.ByWallet[tx.Wallet]++

		if tx.ID != "" {
			uniqueIDs[tx.ID] = struct{}{}
		}

		if tx.RawType == nil || strings.TrimSpace(*tx.RawType) == "" {
			summary.MissingRawTypeCount++
		} else {
			summary.ByRawType[*tx.RawType]++
		}

		if summary.TimestampRange == nil {
			summary.TimestampRange = &TimestampRange{
				From: tx.Timestamp,
				To:   tx.Timestamp,
			}
		} else {
			if tx.Timestamp.Before(summary.TimestampRange.From) {
				summary.TimestampRange.From = tx.Timestamp
			}
			if tx.Timestamp.After(summary.TimestampRange.To) {
				summary.TimestampRange.To = tx.Timestamp
			}
		}

		assetsSeen := make(map[string]struct{})
		if err := accumulateAsset(assetAccumulators, assetsSeen, tx.Sent, "sent"); err != nil {
			return Summary{}, fmt.Errorf("transaction %d sent amount: %w", index, err)
		}
		if err := accumulateAsset(assetAccumulators, assetsSeen, tx.Received, "received"); err != nil {
			return Summary{}, fmt.Errorf("transaction %d received amount: %w", index, err)
		}
		if err := accumulateAsset(assetAccumulators, assetsSeen, tx.Fee, "fee"); err != nil {
			return Summary{}, fmt.Errorf("transaction %d fee amount: %w", index, err)
		}
		for asset := range assetsSeen {
			assetAccumulators[asset].TransactionCount++
		}
	}

	summary.UniqueIDCount = len(uniqueIDs)
	for asset, acc := range assetAccumulators {
		summary.ByAsset[asset] = acc.Snapshot()
	}

	if len(summary.BySource) == 0 {
		summary.BySource = nil
	}
	if len(summary.ByChain) == 0 {
		summary.ByChain = nil
	}
	if len(summary.ByTxType) == 0 {
		summary.ByTxType = nil
	}
	if len(summary.ByWallet) == 0 {
		summary.ByWallet = nil
	}
	if len(summary.ByRawType) == 0 {
		summary.ByRawType = nil
	}
	if len(summary.ByAsset) == 0 {
		summary.ByAsset = nil
	}

	return summary, nil
}

func MarshalSummary(summary Summary) ([]byte, error) {
	summaryJSON, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}
	return summaryJSON, nil
}

func ShellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func matchesFilters(tx types.Transaction, filters Filters) bool {
	if filters.TxID != "" && tx.ID != filters.TxID {
		return false
	}
	if filters.FromTimestamp != nil && tx.Timestamp.Before(*filters.FromTimestamp) {
		return false
	}
	if filters.ToTimestamp != nil && tx.Timestamp.After(*filters.ToTimestamp) {
		return false
	}
	if filters.Wallet != "" && tx.Wallet != filters.Wallet {
		return false
	}
	if filters.Asset != "" && !transactionHasAsset(tx, filters.Asset) {
		return false
	}
	if filters.RawType != "" {
		if tx.RawType == nil || *tx.RawType != filters.RawType {
			return false
		}
	}
	return true
}

func transactionHasAsset(tx types.Transaction, asset string) bool {
	for _, amount := range []*types.AssetAmount{tx.Sent, tx.Received, tx.Fee} {
		if amount != nil && strings.EqualFold(amount.Asset, asset) {
			return true
		}
	}
	return false
}

func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	if isShellSafe(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func isShellSafe(arg string) bool {
	for _, r := range arg {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_/.:=,@+%", r)) {
			return false
		}
	}
	return true
}

type assetSummaryAccumulator struct {
	TransactionCount int
	SentCount        int
	SentAmount       amountAccumulator
	ReceivedCount    int
	ReceivedAmount   amountAccumulator
	FeeCount         int
	FeeAmount        amountAccumulator
}

func (acc assetSummaryAccumulator) Snapshot() AssetSummary {
	return AssetSummary{
		TransactionCount: acc.TransactionCount,
		SentCount:        acc.SentCount,
		SentAmount:       acc.SentAmount.String(),
		ReceivedCount:    acc.ReceivedCount,
		ReceivedAmount:   acc.ReceivedAmount.String(),
		FeeCount:         acc.FeeCount,
		FeeAmount:        acc.FeeAmount.String(),
	}
}

func accumulateAsset(accumulators map[string]*assetSummaryAccumulator, assetsSeen map[string]struct{}, amount *types.AssetAmount, field string) error {
	if amount == nil {
		return nil
	}

	asset := strings.ToUpper(strings.TrimSpace(amount.Asset))
	if asset == "" {
		return nil
	}

	acc, ok := accumulators[asset]
	if !ok {
		acc = &assetSummaryAccumulator{}
		accumulators[asset] = acc
	}

	switch field {
	case "sent":
		acc.SentCount++
		if err := acc.SentAmount.Add(amount.Amount); err != nil {
			return err
		}
	case "received":
		acc.ReceivedCount++
		if err := acc.ReceivedAmount.Add(amount.Amount); err != nil {
			return err
		}
	case "fee":
		acc.FeeCount++
		if err := acc.FeeAmount.Add(amount.Amount); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported asset field %q", field)
	}

	assetsSeen[asset] = struct{}{}
	return nil
}

type amountAccumulator struct {
	total    big.Rat
	maxScale int
	seen     bool
}

func (acc *amountAccumulator) Add(value string) error {
	var rat big.Rat
	if _, ok := rat.SetString(value); !ok {
		return fmt.Errorf("parse decimal %q", value)
	}

	if acc.seen {
		acc.total.Add(&acc.total, &rat)
	} else {
		acc.total.Set(&rat)
		acc.seen = true
	}

	if scale := decimalScale(value); scale > acc.maxScale {
		acc.maxScale = scale
	}
	return nil
}

func (acc amountAccumulator) String() string {
	if !acc.seen || acc.total.Sign() == 0 {
		return "0"
	}

	value := acc.total.FloatString(acc.maxScale)
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "" || value == "-0" {
		return "0"
	}
	return value
}

func decimalScale(value string) int {
	value = strings.TrimSpace(value)
	point := strings.IndexByte(value, '.')
	if point < 0 {
		return 0
	}

	frac := value[point+1:]
	if exponent := strings.IndexAny(frac, "eE"); exponent >= 0 {
		frac = frac[:exponent]
	}
	return len(frac)
}
