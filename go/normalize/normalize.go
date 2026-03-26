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

// Normalize converts chain-specific RawTransactions into the unified schema.
func Normalize(
	raws []fetcher.RawTransaction,
	wallets []string,
	priceProvider *price.Provider,
) ([]types.Transaction, error) {
	var txs []types.Transaction
	var errs []string

	walletSet := make(map[string]bool)
	for _, w := range wallets {
		walletSet[strings.ToLower(w)] = true
	}

	for _, raw := range raws {
		tx, err := normalizeOne(raw, walletSet, priceProvider)
		if err != nil {
			errs = append(errs, fmt.Sprintf("skipping tx %s: %v", raw.ID, err))
			continue
		}
		txs = append(txs, tx)
	}

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "Normalization warnings:\n")
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}

	return txs, nil
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
		// TODO: Hyperliquid perpetual positions are not spot acquisitions.
		// This remains a best-effort proxy until the IR can model perp lots/PnL.
		tx.TxType = types.TxBuy
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
	case "CLOSE LONG", "CLOSE SHORT":
		// TODO: Hyperliquid perpetual closes should reconcile against position
		// state, not a spot inventory queue. Keep the approximation explicit.
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
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

		sentUSD := resolveUSDPrice(raw.Asset, raw.Amount, raw.Timestamp, pp)
		rcvUSD := resolveUSDPrice(raw.Asset2, raw.Amount2, raw.Timestamp, pp)

		tx.Sent = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: sentUSD,
		}
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset2),
			Amount:   raw.Amount2,
			USDValue: rcvUSD,
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

	usdValue := resolveUSDPrice(raw.Asset, raw.Amount, raw.Timestamp, pp)
	movement := classifyAddressMovement(raw, wallets)

	switch movement.txType {
	case types.TxTransferOut:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
		}
	case types.TxSell:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
		}
	case types.TxTransferIn:
		// TODO: Distinguishing income from ordinary inbound transfers on Solana
		// needs richer instruction-level modeling than Helius' summary rows.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
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
