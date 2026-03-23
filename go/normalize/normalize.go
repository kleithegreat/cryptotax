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
	ts := time.Unix(raw.Timestamp, 0).UTC()

	tx := types.Transaction{
		ID:        raw.ID,
		Timestamp: ts,
		Source:    raw.Source,
		Chain:     raw.Chain,
		Wallet:    raw.FromAddr,
	}

	if raw.RawType != "" {
		rawType := raw.RawType
		tx.RawType = &rawType
	}

	switch {
	case raw.Source == types.SourceRobinhood:
		tx = normalizeRobinhood(tx, raw)

	case raw.Source == types.SourceHyperliquid:
		tx = normalizeHyperliquid(tx, raw)

	case raw.Source == types.SourceHelius:
		tx = normalizeHelius(tx, raw, wallets, pp)

	default:
		// EVM chains (Etherscan): amounts are already decimal from the fetcher
		tx = normalizeEVM(tx, raw, wallets, pp)
	}

	return tx, nil
}

func normalizeRobinhood(tx types.Transaction, raw fetcher.RawTransaction) types.Transaction {
	tx.Wallet = "robinhood"

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

func normalizeHyperliquid(tx types.Transaction, raw fetcher.RawTransaction) types.Transaction {
	if raw.RawType == "funding" {
		tx.TxType = types.TxFundingPayment
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
		tx.TxType = types.TxBuy
		tx.Received = &types.AssetAmount{
			Asset:    strings.ToUpper(raw.Asset),
			Amount:   raw.Amount,
			USDValue: usdValue,
		}
	case "CLOSE LONG", "CLOSE SHORT":
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
		tx.Fee = &types.AssetAmount{
			Asset:    raw.FeeAsset,
			Amount:   raw.Fee,
			USDValue: raw.Fee, // fees are in USDC
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

	// Simple transfer
	fromIsOwn := wallets[strings.ToLower(raw.FromAddr)]
	toIsOwn := wallets[strings.ToLower(raw.ToAddr)]

	usdValue := resolveUSDPrice(raw.Asset, raw.Amount, raw.Timestamp, pp)

	switch {
	case fromIsOwn && toIsOwn:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
		}
	case fromIsOwn:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
		}
		cp := raw.ToAddr
		tx.Counterparty = &cp
	case toIsOwn:
		// TODO: default to transfer_in rather than income to avoid overstating
		// taxable income. User can reclassify if it was actually income.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: raw.Amount, USDValue: usdValue,
		}
		cp := raw.FromAddr
		tx.Counterparty = &cp
	default:
		tx.TxType = types.TxTransferIn
	}

	return tx
}

func normalizeEVM(
	tx types.Transaction,
	raw fetcher.RawTransaction,
	wallets map[string]bool,
	pp *price.Provider,
) types.Transaction {
	fromIsOwn := wallets[strings.ToLower(raw.FromAddr)]
	toIsOwn := wallets[strings.ToLower(raw.ToAddr)]

	// Amounts are already in human-readable decimal from the fetcher
	amount := raw.Amount

	usdValue := raw.USDPrice
	if usdValue == "" {
		usdValue = resolveUSDPrice(raw.Asset, amount, raw.Timestamp, pp)
	}

	switch {
	case fromIsOwn && toIsOwn:
		tx.TxType = types.TxTransferOut
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}

	case fromIsOwn && !toIsOwn:
		tx.TxType = types.TxSell
		tx.Sent = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}
		counterparty := raw.ToAddr
		tx.Counterparty = &counterparty

	case !fromIsOwn && toIsOwn:
		// Default to transfer_in, not income — many incoming txs are contract
		// interactions or DEX returns, not taxable income. User can reclassify.
		tx.TxType = types.TxTransferIn
		tx.Received = &types.AssetAmount{
			Asset: strings.ToUpper(raw.Asset), Amount: amount, USDValue: usdValue,
		}
		counterparty := raw.FromAddr
		tx.Counterparty = &counterparty

	default:
		tx.TxType = types.TxTransferIn
	}

	// Gas fee (already in decimal from fetcher)
	if fromIsOwn && raw.Fee != "" {
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
