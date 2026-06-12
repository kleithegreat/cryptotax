package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/normalize"
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
	ZeroUSDValueRows    []FlaggedRow            `json:"zero_usd_value_rows,omitempty"`
	SuspiciousAssetRows []FlaggedRow            `json:"suspicious_asset_rows,omitempty"`
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

type FlaggedRow struct {
	ID        string       `json:"id"`
	Timestamp time.Time    `json:"timestamp"`
	Source    types.Source `json:"source"`
	Chain     types.Chain  `json:"chain"`
	TxType    types.TxType `json:"tx_type"`
	Wallet    string       `json:"wallet"`
	Assets    []string     `json:"assets,omitempty"`
	Fields    []string     `json:"fields,omitempty"`
	Reasons   []string     `json:"reasons,omitempty"`
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

func WriteCaptureArtifacts(outputPath string, payload types.TxPayload, commandLine string, skipped []normalize.SkippedRow) error {
	if err := WritePayload(outputPath, payload); err != nil {
		return err
	}

	commandContents := fmt.Sprintf("# Re-run command\n%s\n", strings.TrimSpace(commandLine))
	commandPath := outputPath + ".command"
	if err := os.WriteFile(commandPath, []byte(commandContents), 0o644); err != nil {
		return fmt.Errorf("write command file %s: %w", commandPath, err)
	}

	skippedPath := outputPath + ".skipped.json"
	if len(skipped) == 0 {
		if err := os.Remove(skippedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale skipped rows %s: %w", skippedPath, err)
		}
		return nil
	}

	skippedJSON, err := json.MarshalIndent(skipped, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skipped rows: %w", err)
	}
	if err := os.WriteFile(skippedPath, append(skippedJSON, '\n'), 0o644); err != nil {
		return fmt.Errorf("write skipped rows %s: %w", skippedPath, err)
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
		summary.ByWallet[types.CanonicalWallet(tx.Wallet)]++

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

		zeroUSDValueRow, err := buildZeroUSDValueRow(tx)
		if err != nil {
			return Summary{}, fmt.Errorf("transaction %d usd_value: %w", index, err)
		}
		if zeroUSDValueRow != nil {
			summary.ZeroUSDValueRows = append(summary.ZeroUSDValueRows, *zeroUSDValueRow)
		}

		suspiciousAssetRow, err := buildSuspiciousAssetRow(tx)
		if err != nil {
			return Summary{}, fmt.Errorf("transaction %d suspicious_asset: %w", index, err)
		}
		if suspiciousAssetRow != nil {
			summary.SuspiciousAssetRows = append(summary.SuspiciousAssetRows, *suspiciousAssetRow)
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
	if filters.Wallet != "" && types.CanonicalWallet(tx.Wallet) != types.CanonicalWallet(filters.Wallet) {
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
		if amount == nil {
			continue
		}
		if strings.EqualFold(amount.Asset, asset) {
			return true
		}
		if amount.AssetCanonical != nil && strings.EqualFold(*amount.AssetCanonical, asset) {
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
	total big.Rat
	seen  bool
}

func (acc *amountAccumulator) Add(value string) error {
	rat, err := decimal.Parse(value)
	if err != nil {
		return err
	}

	if acc.seen {
		acc.total.Add(&acc.total, rat)
	} else {
		acc.total.Set(rat)
		acc.seen = true
	}
	return nil
}

func (acc amountAccumulator) String() string {
	if !acc.seen {
		return "0"
	}
	return decimal.String(&acc.total)
}

type assetComponent struct {
	field  string
	amount *types.AssetAmount
}

func transactionAssetComponents(tx types.Transaction) []assetComponent {
	return []assetComponent{
		{field: "sent", amount: tx.Sent},
		{field: "received", amount: tx.Received},
		{field: "fee", amount: tx.Fee},
	}
}

func buildZeroUSDValueRow(tx types.Transaction) (*FlaggedRow, error) {
	var fields []string
	var assets []string

	for _, component := range transactionAssetComponents(tx) {
		if component.amount == nil {
			continue
		}

		isZero, err := isZeroDecimal(component.amount.USDValue)
		if err != nil {
			return nil, err
		}
		if !isZero {
			continue
		}

		fields = append(fields, component.field)
		assets = appendUniqueString(assets, displayAsset(component.amount.Asset))
	}

	if len(fields) == 0 {
		return nil, nil
	}

	return &FlaggedRow{
		ID:        tx.ID,
		Timestamp: tx.Timestamp,
		Source:    tx.Source,
		Chain:     tx.Chain,
		TxType:    tx.TxType,
		Wallet:    tx.Wallet,
		Assets:    assets,
		Fields:    fields,
	}, nil
}

func buildSuspiciousAssetRow(tx types.Transaction) (*FlaggedRow, error) {
	var assets []string
	var fields []string
	reasonSet := make(map[string]struct{})

	for _, component := range transactionAssetComponents(tx) {
		if component.amount == nil {
			continue
		}

		reasons := suspiciousAssetReasons(component.amount.Asset)
		if len(reasons) == 0 {
			continue
		}

		assets = appendUniqueString(assets, displayAsset(component.amount.Asset))
		fields = appendUniqueString(fields, component.field)
		for _, reason := range reasons {
			reasonSet[reason] = struct{}{}
		}
		if assetIdentityNeedsManualReview(reasons) {
			isZero, err := isZeroDecimal(component.amount.USDValue)
			if err != nil {
				return nil, err
			}
			if isZero {
				reasonSet["unresolved_asset_valuation"] = struct{}{}
			}
		}
	}

	if len(assets) == 0 {
		return nil, nil
	}

	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)

	return &FlaggedRow{
		ID:        tx.ID,
		Timestamp: tx.Timestamp,
		Source:    tx.Source,
		Chain:     tx.Chain,
		TxType:    tx.TxType,
		Wallet:    tx.Wallet,
		Assets:    assets,
		Fields:    fields,
		Reasons:   reasons,
	}, nil
}

func assetIdentityNeedsManualReview(reasons []string) bool {
	for _, reason := range reasons {
		switch reason {
		case "blank_asset_symbol", "placeholder_asset_symbol", "address_like_asset_symbol":
			return true
		}
	}
	return false
}

func isZeroDecimal(value string) (bool, error) {
	sign, err := decimal.Sign(value)
	if err != nil {
		return false, err
	}
	return sign == 0, nil
}

func suspiciousAssetReasons(asset string) []string {
	trimmed := strings.TrimSpace(asset)
	if trimmed == "" {
		return []string{"blank_asset_symbol"}
	}

	var reasons []string
	upper := strings.ToUpper(trimmed)
	addressLike := looksLikeHexAddress(trimmed) || looksLikeMintLikeAsset(trimmed)

	if upper == "UNKNOWN" || upper == "UNK" || upper == "?" {
		reasons = append(reasons, "placeholder_asset_symbol")
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		reasons = append(reasons, "whitespace_in_asset_symbol")
	}
	if !addressLike && trimmed != upper {
		reasons = append(reasons, "non_canonical_asset_case")
	}
	if addressLike {
		reasons = append(reasons, "address_like_asset_symbol")
	}

	return reasons
}

func looksLikeHexAddress(asset string) bool {
	if len(asset) != 42 || !strings.HasPrefix(asset, "0x") {
		return false
	}

	for _, r := range asset[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func looksLikeBase58Mint(asset string) bool {
	if len(asset) < 32 || len(asset) > 44 {
		return false
	}

	for _, r := range asset {
		if !strings.ContainsRune("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz", r) {
			return false
		}
	}
	return true
}

func looksLikeMintLikeAsset(asset string) bool {
	if looksLikeBase58Mint(asset) {
		return true
	}
	if len(asset) < 32 || len(asset) > 44 {
		return false
	}

	// Some Solana asset identifiers are still clearly mint-like long strings even
	// when they do not pass strict base58 validation (for example, uppercase I).
	hasLetter := false
	hasDigit := false
	for _, r := range asset {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			return false
		}
	}

	return hasLetter && hasDigit
}

func displayAsset(asset string) string {
	trimmed := strings.TrimSpace(asset)
	if trimmed == "" {
		return "<blank>"
	}
	return trimmed
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
