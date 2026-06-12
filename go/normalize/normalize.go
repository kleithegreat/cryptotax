package normalize

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/decimal"
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
		walletSet[types.CanonicalWallet(w)] = true
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
	if raw.Timestamp <= 0 {
		// A zero/negative unix timestamp is a missing source field, not the
		// year 1970; letting it through would warp FIFO order and holding
		// periods for the whole asset.
		return types.Transaction{}, fmt.Errorf("missing or invalid timestamp %d", raw.Timestamp)
	}

	ts := time.Unix(raw.Timestamp, 0).UTC()

	tx := types.Transaction{
		ID:        raw.ID,
		Timestamp: ts,
		Source:    raw.Source,
		Chain:     raw.Chain,
		Wallet:    types.CanonicalWallet(raw.Wallet),
	}

	if raw.RawType != "" {
		rawType := raw.RawType
		tx.RawType = &rawType
	}
	if raw.Market != "" {
		market := raw.Market
		tx.Market = &market
	}
	if raw.EventGroupID != "" {
		eventGroupID := raw.EventGroupID
		tx.EventGroupID = &eventGroupID
	} else if groupID := defaultEventGroupID(raw); groupID != "" {
		tx.EventGroupID = &groupID
	}
	if raw.SplitReason != "" {
		splitReason := raw.SplitReason
		tx.SplitReason = &splitReason
	}

	var err error
	switch raw.Source {
	case types.SourceRobinhood:
		tx, err = normalizeRobinhood(tx, raw)

	case types.SourceHyperliquid:
		tx, err = normalizeHyperliquid(tx, raw, pp)

	case types.SourceHelius:
		tx, err = normalizeHelius(tx, raw, wallets, pp)

	default:
		// EVM chains (Etherscan): amounts are already decimal from the fetcher
		tx, err = normalizeEVM(tx, raw, wallets, pp)
	}
	if err != nil {
		return types.Transaction{}, err
	}

	return finalizeTransaction(tx)
}

// finalizeTransaction enforces the IR contract at the single exit point of
// normalization: canonical plain decimal strings (no scientific notation —
// the Haskell parser rejects it), non-negative amounts and valuations, and a
// classified tx_type. Violations skip the row with a diagnostic instead of
// corrupting the financial core.
func finalizeTransaction(tx types.Transaction) (types.Transaction, error) {
	if tx.TxType == "" {
		return types.Transaction{}, fmt.Errorf("row was not classified")
	}

	canon := func(aa *types.AssetAmount, field string) error {
		if aa == nil {
			return nil
		}
		amount, err := decimal.Canon(aa.Amount)
		if err != nil {
			return fmt.Errorf("%s amount: %w", field, err)
		}
		if sign, _ := decimal.Sign(amount); sign < 0 {
			return fmt.Errorf("%s amount %q is negative", field, aa.Amount)
		}
		usd, err := decimal.Canon(aa.USDValue)
		if err != nil {
			return fmt.Errorf("%s usd_value: %w", field, err)
		}
		if sign, _ := decimal.Sign(usd); sign < 0 {
			return fmt.Errorf("%s usd_value %q is negative", field, aa.USDValue)
		}
		aa.Amount, aa.USDValue = amount, usd
		return nil
	}

	if err := canon(tx.Sent, "sent"); err != nil {
		return types.Transaction{}, err
	}
	if err := canon(tx.Received, "received"); err != nil {
		return types.Transaction{}, err
	}
	if err := canon(tx.Fee, "fee"); err != nil {
		return types.Transaction{}, err
	}
	if tx.ClosedPnl != nil {
		pnl, err := decimal.Canon(*tx.ClosedPnl) // negative is a valid loss
		if err != nil {
			return types.Transaction{}, fmt.Errorf("closed_pnl: %w", err)
		}
		tx.ClosedPnl = &pnl
	}

	return tx, nil
}

func normalizeRobinhood(tx types.Transaction, raw fetcher.RawTransaction) (types.Transaction, error) {
	switch {
	case strings.HasPrefix(raw.RawType, "1099-DA-BUY"):
		tx.TxType = types.TxBuy
		tx.Received = &types.AssetAmount{
			Asset:    raw.Asset,
			Amount:   raw.Amount,
			USDValue: raw.USDPrice, // total cost basis from 1099-DA
		}

	case strings.HasPrefix(raw.RawType, "1099-DA-SELL"):
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset:    raw.Asset,
			Amount:   raw.Amount,
			USDValue: raw.USDPrice, // total proceeds from 1099-DA
		}

	default:
		// Never guess a tax classification for an unknown row type.
		return types.Transaction{}, fmt.Errorf("unsupported Robinhood raw type %q", raw.RawType)
	}

	return tx, nil
}

func normalizeHyperliquid(tx types.Transaction, raw fetcher.RawTransaction, pp *price.Provider) (types.Transaction, error) {
	if raw.RawType == "funding" {
		tx.TxType = types.TxFundingPayment
		if tx.Market == nil && raw.Asset != "" {
			market := strings.ToUpper(raw.Asset)
			tx.Market = &market
		}
		sign, err := decimal.Sign(raw.Amount)
		if err != nil {
			return types.Transaction{}, fmt.Errorf("funding amount: %w", err)
		}
		if sign < 0 {
			// Funding paid is an ordinary expense, not a spot trade or fee.
			// Preserve it as an explicit outbound funding leg so the core can
			// surface the unsupported expense instead of dropping it silently.
			amount, err := decimal.Abs(raw.Amount)
			if err != nil {
				return types.Transaction{}, fmt.Errorf("funding amount: %w", err)
			}
			tx.Sent = &types.AssetAmount{
				Asset:    "USDC",
				Amount:   amount,
				USDValue: amount, // USDC ≈ 1 USD
			}
			return tx, nil
		}
		tx.Received = &types.AssetAmount{
			Asset:    "USDC",
			Amount:   raw.Amount,
			USDValue: raw.Amount, // USDC ≈ 1 USD
		}
		return tx, nil
	}

	usdValue, err := decimal.Mul(raw.Amount, raw.USDPrice)
	if err != nil {
		return types.Transaction{}, fmt.Errorf("fill value (%q × %q): %w", raw.Amount, raw.USDPrice, err)
	}

	asset := strings.ToUpper(raw.Asset)

	// Fill directions are matched explicitly. The previous catch-all turned
	// every unrecognized direction — including spot sells — into a spot BUY,
	// inverting real trades. Unknown directions now skip with a diagnostic
	// so they surface instead of being guessed.
	switch strings.ToUpper(raw.RawType) {
	case "OPEN LONG", "OPEN SHORT":
		// Perp opens are quarantined from spot FIFO. No phantom lots are created.
		tx.TxType = types.TxPerpOpen
		tx.Received = &types.AssetAmount{
			Asset:    asset,
			Amount:   raw.Amount,
			USDValue: usdValue,
		}

	case "CLOSE LONG", "CLOSE SHORT", "LONG > SHORT", "SHORT > LONG":
		// Perp closes carry ClosedPnl from the exchange for realized PnL
		// output. Direction flips ("Long > Short") close the existing
		// position — the exchange reports the realized PnL of the closed
		// side on the flip fill — so they are modeled as closes; the newly
		// opened opposite side has no tax event until it closes.
		tx.TxType = types.TxPerpClose
		tx.Sent = &types.AssetAmount{
			Asset:    asset,
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
		if raw.ClosedPnl != "" {
			closedPnl := raw.ClosedPnl
			tx.ClosedPnl = &closedPnl
		}

	case "BUY":
		// Spot buy: USDC out, asset in.
		tx.TxType = types.TxSwap
		tx.Sent = &types.AssetAmount{
			Asset:    "USDC",
			Amount:   usdValue,
			USDValue: usdValue,
		}
		tx.Received = &types.AssetAmount{
			Asset:    asset,
			Amount:   raw.Amount,
			USDValue: usdValue,
		}

	case "SELL":
		// Spot sell: asset out, USDC in.
		tx.TxType = types.TxSwap
		tx.Sent = &types.AssetAmount{
			Asset:    asset,
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
		tx.Received = &types.AssetAmount{
			Asset:    "USDC",
			Amount:   usdValue,
			USDValue: usdValue,
		}

	default:
		return types.Transaction{}, fmt.Errorf("unsupported Hyperliquid fill direction %q", raw.RawType)
	}

	if raw.Fee != "" {
		feeAsset := strings.ToUpper(raw.FeeAsset)
		feeUSD := raw.Fee
		if feeAsset != "" && feeAsset != "USDC" {
			feeUSD = resolveUSDPrice(feeAsset, "", raw.Fee, raw.Timestamp, pp)
		}
		tx.Fee = &types.AssetAmount{
			Asset:    feeAsset,
			Amount:   raw.Fee,
			USDValue: feeUSD,
		}
	}

	return tx, nil
}

func defaultEventGroupID(raw fetcher.RawTransaction) string {
	if strings.TrimSpace(raw.ID) == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", raw.Source, raw.ID)
}

func normalizeHelius(
	tx types.Transaction,
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) (types.Transaction, error) {
	// Helius swaps have Asset (sent) and Asset2 (received)
	if raw.Asset2 != "" {
		tx.TxType = types.TxSwap

		sentUSD := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset, raw.AssetSymbol), heliusMint(raw.Asset), raw.Amount, raw.Timestamp, pp)
		rcvUSD := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset2, raw.Asset2Symbol), heliusMint(raw.Asset2), raw.Amount2, raw.Timestamp, pp)

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

		attachSourceFee(&tx, raw, "SOL", pp)
		return tx, nil
	}

	usdValue := resolveUSDPrice(heliusPriceLookupAsset(raw.Asset, raw.AssetSymbol), heliusMint(raw.Asset), raw.Amount, raw.Timestamp, pp)
	displayAsset := heliusDisplayAsset(raw.Asset, raw.AssetSymbol)
	canonical := heliusCanonical(raw.Asset, raw.AssetSymbol)
	movement, err := classifyAddressMovement(raw, wallets)
	if err != nil {
		return types.Transaction{}, err
	}

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

	attachSourceFee(&tx, raw, "SOL", pp)
	return tx, nil
}

func normalizeEVM(
	tx types.Transaction,
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) (types.Transaction, error) {
	// Amounts are already in human-readable decimal from the fetcher
	amount := raw.Amount

	usdValue := raw.USDPrice
	if usdValue == "" {
		usdValue = resolveUSDPrice(raw.Asset, raw.AssetCanonical, amount, raw.Timestamp, pp)
	}

	// Display symbols are case-folded for stable grouping; the contract
	// address (when present) preserves the exact source identity.
	displayAsset := strings.ToUpper(raw.Asset)
	var canonical *string
	if raw.AssetCanonical != "" {
		c := raw.AssetCanonical
		canonical = &c
	}

	movement, err := classifyAddressMovement(raw, wallets)
	if err != nil {
		return types.Transaction{}, err
	}

	switch movement.txType {
	case types.TxTransferOut:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: amount, USDValue: usdValue,
		}
	case types.TxSell:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: amount, USDValue: usdValue,
		}
	case types.TxTransferIn:
		// Default to transfer_in, not income — many inbound EVM rows are
		// transfer returns or contract outputs, not clear taxable income.
		// TODO: Reconstruct multi-leg EVM swaps/bridges per tx hash instead of
		// classifying isolated token movements one row at a time.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: displayAsset, AssetCanonical: canonical, Amount: amount, USDValue: usdValue,
		}
	default:
		tx.TxType = movement.txType
	}

	if movement.counterparty != nil {
		tx.Counterparty = movement.counterparty
	}

	attachSourceFee(&tx, raw, "ETH", pp)
	return tx, nil
}

// attachSourceFee attaches the network fee when the fetcher reported one.
// Fetchers only set Fee on rows whose wallet actually paid it (EVM: the tx
// initiator; Solana: the fee payer), so presence of fee data is the whole
// test — no per-direction guessing here.
func attachSourceFee(tx *types.Transaction, raw fetcher.RawTransaction, defaultAsset string, pp *price.Provider) {
	if raw.Fee == "" {
		return
	}
	feeAsset := raw.FeeAsset
	if feeAsset == "" {
		feeAsset = defaultAsset
	}
	feeUSD := resolveUSDPrice(feeAsset, "", raw.Fee, raw.Timestamp, pp)
	tx.Fee = &types.AssetAmount{
		Asset:    feeAsset,
		Amount:   raw.Fee,
		USDValue: feeUSD,
	}
}

func resolveUSDPrice(asset, canonical, amount string, unixTS int64, pp *price.Provider) string {
	if pp == nil {
		return "0"
	}
	ts := time.Unix(unixTS, 0).UTC()
	p, err := pp.Lookup(asset, canonical, ts)
	if err != nil {
		return "0"
	}
	value, err := decimal.Mul(amount, p)
	if err != nil {
		return "0"
	}
	return value
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

// heliusMint passes the mint as the canonical pricing identity, except for
// native SOL which has no mint.
func heliusMint(assetID string) string {
	assetID = strings.TrimSpace(assetID)
	if assetID == "SOL" {
		return ""
	}
	return assetID
}

type addressMovement struct {
	txType       types.TxType
	counterparty *string
}

func classifyAddressMovement(raw fetcher.RawTransaction, wallets map[string]bool) (addressMovement, error) {
	wallet := types.CanonicalWallet(raw.Wallet)
	from := types.CanonicalWallet(raw.FromAddr)
	to := types.CanonicalWallet(raw.ToAddr)

	walletIsSender := wallet != "" && from == wallet
	walletIsReceiver := wallet != "" && to == wallet
	fromIsOwn := wallets[from]
	toIsOwn := wallets[to]

	switch {
	case walletIsSender && toIsOwn:
		return addressMovement{
			txType:       types.TxTransferOut,
			counterparty: stringPtr(raw.ToAddr),
		}, nil
	case walletIsSender:
		return addressMovement{
			txType:       types.TxSell,
			counterparty: stringPtr(raw.ToAddr),
		}, nil
	case walletIsReceiver && fromIsOwn:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}, nil
	case walletIsReceiver:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}, nil
	case fromIsOwn && toIsOwn:
		return addressMovement{
			txType:       types.TxTransferOut,
			counterparty: stringPtr(raw.ToAddr),
		}, nil
	case fromIsOwn:
		return addressMovement{
			txType:       types.TxSell,
			counterparty: stringPtr(raw.ToAddr),
		}, nil
	case toIsOwn:
		return addressMovement{
			txType:       types.TxTransferIn,
			counterparty: stringPtr(raw.FromAddr),
		}, nil
	default:
		// The row references neither the wallet nor any owned address —
		// classifying it would be pure invention.
		return addressMovement{}, fmt.Errorf("row does not involve the wallet or any owned address (from %q, to %q)", raw.FromAddr, raw.ToAddr)
	}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	ptr := value
	return &ptr
}
