package normalize

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/fetcher"
	"github.com/kevin/cryptotax/price"
	"github.com/kevin/cryptotax/types"
)

// SkippedRow records a raw transaction that could not be normalized.
type SkippedRow struct {
	TxID    string       `json:"tx_id"`
	Source  types.Source `json:"source"`
	Chain   types.Chain  `json:"chain"`
	RawType string       `json:"raw_type,omitempty"`
	Reason  string       `json:"reason"`
}

// NormalizeResult bundles normalized transactions with structured diagnostics
// about any rows that were skipped during normalization.
type NormalizeResult struct {
	Transactions []types.Transaction
	Skipped      []SkippedRow
}

// NormalizeWithDiagnostics converts raw transactions into normalized form and
// returns structured information about any rows that were skipped.
func NormalizeWithDiagnostics(
	raws []fetcher.RawTransaction,
	wallets []string,
	priceProvider *price.Provider,
) NormalizeResult {
	var result NormalizeResult

	walletSet := make(map[string]bool)
	for _, w := range wallets {
		walletSet[strings.ToLower(w)] = true
	}

	for _, raw := range raws {
		tx, err := normalizeOne(raw, walletSet, priceProvider)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedRow{
				TxID:    raw.ID,
				Source:  raw.Source,
				Chain:   raw.Chain,
				RawType: raw.RawType,
				Reason:  err.Error(),
			})
			continue
		}
		result.Transactions = append(result.Transactions, tx)
	}

	return result
}

// Normalize converts chain-specific RawTransactions into the unified schema.
// Skipped rows are printed to stderr. Use NormalizeWithDiagnostics for
// programmatic access to skip information.
func Normalize(
	raws []fetcher.RawTransaction,
	wallets []string,
	priceProvider *price.Provider,
) ([]types.Transaction, error) {
	result := NormalizeWithDiagnostics(raws, wallets, priceProvider)

	if len(result.Skipped) > 0 {
		fmt.Fprintf(os.Stderr, "Normalization warnings:\n")
		for _, s := range result.Skipped {
			fmt.Fprintf(os.Stderr, "  skipping tx %s: %s\n", s.TxID, s.Reason)
		}
	}

	return result.Transactions, nil
}

func normalizeOne(
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) (types.Transaction, error) {
	if raw.Wallet == "" {
		return types.Transaction{}, fmt.Errorf("missing wallet on raw transaction")
	}

	ts := time.Unix(raw.Timestamp, 0).UTC()

	tx := types.Transaction{
		ID:        raw.ID,
		Timestamp: ts,
		Source:    raw.Source,
		Chain:     raw.Chain,
		Wallet:    raw.Wallet,
	}

	if raw.RawType != "" {
		rawType := raw.RawType
		tx.RawType = &rawType
	}

	switch {
	case raw.Source == types.SourceRobinhood:
		tx = normalizeRobinhood(tx, raw)

	case raw.Source == types.SourceHyperliquid:
		tx = normalizeHyperliquid(tx, raw, pp)

	case raw.Source == types.SourceHelius:
		tx = normalizeHelius(tx, raw, wallets, pp)

	default:
		// EVM chains (Etherscan): amounts are already decimal from the fetcher
		tx = normalizeEVM(tx, raw, wallets, pp)
	}

	return tx, nil
}

func normalizeRobinhood(tx types.Transaction, raw fetcher.RawTransaction) types.Transaction {
	switch {
	case strings.Contains(raw.RawType, "BUY"):
		tx.TxType = types.TxBuy
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: raw.USDPrice, // total cost basis from 1099-DA
		}

	case strings.Contains(raw.RawType, "SELL"):
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: raw.USDPrice, // total proceeds from 1099-DA
		}

	default:
		tx.TxType = types.TxBuy
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: raw.USDPrice,
		}
	}

	return tx
}

func normalizeHyperliquid(tx types.Transaction, raw fetcher.RawTransaction, pp *price.Provider) types.Transaction {
	if raw.RawType == "funding" {
		tx.TxType = types.TxFundingPayment
		if isNegativeDecimal(raw.Amount) {
			// Funding paid is an ordinary expense, not a spot trade or fee.
			// Preserve it as an explicit outbound funding leg so the core can
			// surface the unsupported expense instead of dropping it silently.
			amount := absDecimalString(raw.Amount)
			tx.Sent = &types.AssetAmount{
				Asset:    "USDC",
				Amount:   amount,
				USDValue: amount, // USDC ≈ 1 USD
			}
			return tx
		}
		tx.Received = &types.AssetAmount{
			Asset:    "USDC",
			Amount:   raw.Amount,
			USDValue: raw.Amount, // USDC ≈ 1 USD
		}
		return tx
	}

	usdValue := multiplyStrings(raw.Amount, raw.USDPrice)

	switch strings.ToUpper(raw.RawType) {
	case "OPEN LONG", "OPEN SHORT":
		// Perp opens are quarantined from spot FIFO. No phantom lots are created.
		tx.TxType = types.TxPerpOpen
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
	case "CLOSE LONG", "CLOSE SHORT":
		// Perp closes carry ClosedPnl from the exchange for realized PnL output.
		tx.TxType = types.TxPerpClose
		tx.Sent = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
		if raw.ClosedPnl != "" {
			closedPnl := raw.ClosedPnl
			tx.ClosedPnl = &closedPnl
		}
	default:
		// Spot trade
		tx.TxType = types.TxSwap
		tx.Sent = &types.AssetAmount{
			Asset:    "USDC",
			Amount:   usdValue,
			USDValue: usdValue,
		}
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
	}

	if raw.Fee != "" {
		feeAsset := strings.ToUpper(raw.FeeAsset)
		feeUSD := raw.Fee
		if feeAsset != "" && feeAsset != "USDC" {
			feeUSD = resolveUSDPrice(feeAsset, raw.Fee, raw.Timestamp, pp)
		}
		tx.Fee = &types.AssetAmount{
			Asset:    feeAsset,
			Amount:   raw.Fee,
			USDValue: feeUSD,
		}
	}

	return tx
}

func normalizeHelius(
	tx types.Transaction,
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) types.Transaction {
	// Helius swaps have Asset (sent) and Asset2 (received)
	if raw.Asset2 != "" {
		tx.TxType = types.TxSwap

		sentUSD := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset, raw.AssetSymbol), raw.Amount, raw.Timestamp, pp)
		rcvUSD := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset2, raw.Asset2Symbol), raw.Amount2, raw.Timestamp, pp)

		tx.Sent = &types.AssetAmount{
			Asset:          heliusDisplayAsset(raw.Asset, raw.AssetSymbol),
			AssetCanonical: heliusCanonical(raw.Asset, raw.AssetSymbol),
			Amount:         raw.Amount,
			USDValue:       sentUSD,
		}
		tx.Received = &types.AssetAmount{
			Asset:          heliusDisplayAsset(raw.Asset2, raw.Asset2Symbol),
			AssetCanonical: heliusCanonical(raw.Asset2, raw.Asset2Symbol),
			Amount:         raw.Amount2,
			USDValue:       rcvUSD,
		}

		if raw.Fee != "" {
			feeUSD := resolveUSDPrice("SOL", raw.Fee, raw.Timestamp, pp)
			tx.Fee = &types.AssetAmount{
				Asset:    "SOL",
				Amount:   raw.Fee,
				USDValue: feeUSD,
			}
		}

		return tx
	}

	usdValue := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset, raw.AssetSymbol), raw.Amount, raw.Timestamp, pp)
	displayAsset := heliusDisplayAsset(raw.Asset, raw.AssetSymbol)
	canonical := heliusCanonical(raw.Asset, raw.AssetSymbol)
	movement := classifyAddressMovement(raw, wallets)

	switch movement.txType {
	case types.TxTransferOut:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: raw.Amount, USDValue: usdValue,
		}
	case types.TxSell:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: raw.Amount, USDValue: usdValue,
		}
	case types.TxTransferIn:
		// TODO: Distinguishing income from ordinary inbound transfers on Solana
		// needs richer instruction-level modeling than Helius' summary rows.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: raw.Amount, USDValue: usdValue,
		}
	default:
		tx.TxType = movement.txType
	}

	if movement.counterparty != nil {
		tx.Counterparty = movement.counterparty
	}

	if raw.Fee != "" && movement.attachFee {
		feeUSD := resolveUSDPrice("SOL", raw.Fee, raw.Timestamp, pp)
		tx.Fee = &types.AssetAmount{
			Asset:    "SOL",
			Amount:   raw.Fee,
			USDValue: feeUSD,
		}
	}

	return tx
}

func normalizeEVM(
	tx types.Transaction,
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) types.Transaction {
	// Amounts are already in human-readable decimal from the fetcher
	amount := raw.Amount

	usdValue := raw.USDPrice
	if usdValue == "" {
		usdValue = resolveUSDPrice(raw.Asset, amount, raw.Timestamp, pp)
	}

	movement := classifyAddressMovement(raw, wallets)

	switch movement.txType {
	case types.TxTransferOut:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}
	case types.TxSell:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}
	case types.TxTransferIn:
		// Default to transfer_in, not income — many inbound EVM rows are
		// transfer returns or contract outputs, not clear taxable income.
		// TODO: Reconstruct multi-leg EVM swaps/bridges per tx hash instead of
		// classifying isolated token movements one row at a time.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}
	default:
		tx.TxType = movement.txType
	}

	if movement.counterparty != nil {
		tx.Counterparty = movement.counterparty
	}

	if raw.Fee != "" && movement.attachFee {
		feeUSD := resolveUSDPrice("ETH", raw.Fee, raw.Timestamp, pp)
		tx.Fee = &types.AssetAmount{
			Asset:    "ETH",
			Amount:   raw.Fee,
			USDValue: feeUSD,
		}
	}

	return tx
}

func resolveUSDPrice(asset, amount string, unixTS int64, pp *price.Provider) string {
	if pp == nil {
		return "0"
	}
	ts := time.Unix(unixTS, 0).UTC()
	p, err := pp.Lookup(asset, ts)
	if err != nil {
		return "0"
	}
	return multiplyStrings(amount, p)
}

func heliusDisplayAsset(assetID, symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if symbol != "" {
		return strings.ToUpper(symbol)
	}

	// Helius token transfers currently document mint addresses, not canonical
	// ticker symbols. Preserve the exact source identifier when no separate
	// source-backed symbol is available instead of uppercasing it into a symbol-
	// like string.
	return strings.TrimSpace(assetID)
}

// heliusCanonical returns the mint as the canonical identity when a
// source-backed symbol is present (meaning display and canonical differ).
// Returns nil when they are the same (no symbol, display = mint already).
func heliusCanonical(assetID, symbol string) *string {
	symbol = strings.TrimSpace(symbol)
	if symbol != "" {
		// Display is the symbol; canonical is the mint.
		id := strings.TrimSpace(assetID)
		return &id
	}
	// Display is already the mint; no separate canonical needed.
	return nil
}

func heliusPriceLookupAsset(assetID, symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if symbol != "" {
		return symbol
	}
	return strings.TrimSpace(assetID)
}

// multiplyStrings multiplies two decimal strings using exact arithmetic.
func multiplyStrings(a, b string) string {
	ra, ok1 := new(big.Rat).SetString(a)
	rb, ok2 := new(big.Rat).SetString(b)
	if !ok1 || !ok2 {
		return "0"
	}
	result := new(big.Rat).Mul(ra, rb)
	return result.FloatString(8)
}

type addressMovement struct {
	txType       types.TxType
	counterparty *string
	attachFee    bool
}

func classifyAddressMovement(raw fetcher.RawTransaction, wallets map[string]bool) addressMovement {
	wallet := strings.ToLower(raw.Wallet)
	from := strings.ToLower(raw.FromAddr)
	to := strings.ToLower(raw.ToAddr)

	walletIsSender := wallet != "" && from == wallet
	walletIsReceiver := wallet != "" && to == wallet
	fromIsOwn := wallets[from]
	toIsOwn := wallets[to]

	switch {
	case walletIsSender && toIsOwn:
		return addressMovement{
			txType:       types.TxTransferOut,
			counterparty: stringPtr(raw.ToAddr),
			attachFee:    true,
		}
	case walletIsSender:
		return addressMovement{
			txType:       types.TxSell,
			counterparty: stringPtr(raw.ToAddr),
			attachFee:    true,
		}
	case walletIsReceiver && fromIsOwn:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}
	case walletIsReceiver:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}
	case fromIsOwn && toIsOwn:
		return addressMovement{
			txType:       types.TxTransferOut,
			counterparty: stringPtr(raw.ToAddr),
			attachFee:    true,
		}
	case fromIsOwn:
		return addressMovement{
			txType:       types.TxSell,
			counterparty: stringPtr(raw.ToAddr),
			attachFee:    true,
		}
	case toIsOwn:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}
	default:
		return addressMovement{txType: types.TxTransferIn}
	}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	ptr := value
	return &ptr
}

func isNegativeDecimal(value string) bool {
	r, ok := new(big.Rat).SetString(value)
	return ok && r.Sign() < 0
}

func absDecimalString(value string) string {
	if strings.HasPrefix(value, "-") {
		return strings.TrimPrefix(value, "-")
	}
	return value
}
