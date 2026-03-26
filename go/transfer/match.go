package transfer

import (
	"math/big"
	"strings"
	"time"

	"github.com/kevin/cryptotax/types"
)

const (
	ownTransferAmountTolerance         = 0.01
	confirmedBridgeAmountTolerance     = 0.01
	ownTransferTimestampWindow         = 30 * time.Minute
	confirmedBridgeTimestampWindow     = 10 * time.Minute
	confirmedBridgeOutRawTypePrefix    = "bridge("
	confirmedBridgeInRawTypePrefix     = "fillrelay("
	confirmedBridgeOutCounterpartyAddr = "0x9a47f3289794e9bbc6a3c571f6d96ad4e7baed16"
	confirmedBridgeInCounterpartyAddr  = "0x07ae8551be970cb1cca11dd7a11f47ae82e70e67"
)

// MatchTransfers identifies pairs of transfer_out + transfer_in transactions
// between the user's own wallets and marks them as non-taxable transfers.
//
// Matching heuristic:
//  1. Same asset
//  2. Amounts within 1% of each other (gas fees cause slight differences)
//  3. Timestamps within 30 minutes of each other
//  4. Counterparties explicitly point at the other owned wallet
//
// It also contains one evidence-driven bridge exception for the confirmed EVM
// same-asset bridge case: an owned-wallet sell can be relabeled to
// transfer_out only when it matches a cross-chain fillRelay-style inbound row
// under the provider-shaped raw-type and counterparty constraints below.
//
// TODO: Cross-chain bridges and same-hash DEX swaps need richer modeling than
// this address-level matcher. Keep this conservative so ordinary sells/income
// are not silently relabeled as non-taxable transfers.
func MatchTransfers(txs []types.Transaction, wallets []string) []types.Transaction {
	ownWallets := make(map[string]struct{}, len(wallets))
	for _, wallet := range wallets {
		ownWallets[strings.ToLower(wallet)] = struct{}{}
	}

	matched := make(map[int]bool)

	for i := range txs {
		if matched[i] || txs[i].Sent == nil {
			continue
		}

		for j := range txs {
			if i == j || matched[j] {
				continue
			}

			if isOwnWalletTransferPair(ownWallets, txs[i], txs[j]) {
				matched[i] = true
				matched[j] = true
				break
			}

			if !isConfirmedBridgeTransferPair(ownWallets, txs[i], txs[j]) {
				continue
			}

			txs[i].TxType = types.TxTransferOut
			matched[i] = true
			matched[j] = true
			break
		}
	}

	return txs
}

func isOwnWalletRef(ownWallets map[string]struct{}, wallet string) bool {
	_, ok := ownWallets[strings.ToLower(wallet)]
	return ok
}

func isOwnCounterparty(ownWallets map[string]struct{}, counterparty *string) bool {
	if counterparty == nil {
		return false
	}
	_, ok := ownWallets[strings.ToLower(*counterparty)]
	return ok
}

func counterpartiesMatch(left, right types.Transaction) bool {
	if left.Counterparty == nil || right.Counterparty == nil {
		return false
	}
	return strings.EqualFold(*left.Counterparty, right.Wallet) &&
		strings.EqualFold(*right.Counterparty, left.Wallet)
}

func isOwnWalletTransferPair(
	ownWallets map[string]struct{},
	left types.Transaction,
	right types.Transaction,
) bool {
	if left.TxType != types.TxTransferOut || left.Sent == nil {
		return false
	}
	if right.TxType != types.TxTransferIn || right.Received == nil {
		return false
	}
	if !isOwnWalletRef(ownWallets, left.Wallet) || !isOwnCounterparty(ownWallets, left.Counterparty) {
		return false
	}
	if !isOwnWalletRef(ownWallets, right.Wallet) || !isOwnCounterparty(ownWallets, right.Counterparty) {
		return false
	}
	if !counterpartiesMatch(left, right) {
		return false
	}
	if !sameAsset(left.Sent.Asset, right.Received.Asset) {
		return false
	}
	if !amountsClose(left.Sent.Amount, right.Received.Amount, ownTransferAmountTolerance) {
		return false
	}

	return timestampsClose(left.Timestamp, right.Timestamp, ownTransferTimestampWindow)
}

func isConfirmedBridgeTransferPair(
	ownWallets map[string]struct{},
	left types.Transaction,
	right types.Transaction,
) bool {
	if !isConfirmedBridgeTransferOutCandidate(ownWallets, left) {
		return false
	}
	if !isConfirmedBridgeTransferInCandidate(ownWallets, right) {
		return false
	}
	if !strings.EqualFold(left.Wallet, right.Wallet) {
		return false
	}
	if left.Chain == right.Chain {
		return false
	}
	if left.Source != types.SourceEtherscan || right.Source != types.SourceEtherscan {
		return false
	}
	if !sameAsset(left.Sent.Asset, right.Received.Asset) {
		return false
	}
	if !amountsClose(left.Sent.Amount, right.Received.Amount, confirmedBridgeAmountTolerance) {
		return false
	}

	return timestampsClose(left.Timestamp, right.Timestamp, confirmedBridgeTimestampWindow)
}

func isConfirmedBridgeTransferOutCandidate(
	ownWallets map[string]struct{},
	tx types.Transaction,
) bool {
	if tx.TxType != types.TxSell || tx.Sent == nil || tx.Counterparty == nil || tx.RawType == nil {
		return false
	}
	if !isOwnWalletRef(ownWallets, tx.Wallet) || isOwnCounterparty(ownWallets, tx.Counterparty) {
		return false
	}
	if !hasRawTypePrefix(tx.RawType, confirmedBridgeOutRawTypePrefix) {
		return false
	}
	if !isPositiveDecimal(tx.Sent.Amount) {
		return false
	}

	return strings.EqualFold(*tx.Counterparty, confirmedBridgeOutCounterpartyAddr)
}

func isConfirmedBridgeTransferInCandidate(
	ownWallets map[string]struct{},
	tx types.Transaction,
) bool {
	if tx.TxType != types.TxTransferIn || tx.Received == nil || tx.Counterparty == nil || tx.RawType == nil {
		return false
	}
	if !isOwnWalletRef(ownWallets, tx.Wallet) || isOwnCounterparty(ownWallets, tx.Counterparty) {
		return false
	}
	if !hasRawTypePrefix(tx.RawType, confirmedBridgeInRawTypePrefix) {
		return false
	}
	if !isPositiveDecimal(tx.Received.Amount) {
		return false
	}

	return strings.EqualFold(*tx.Counterparty, confirmedBridgeInCounterpartyAddr)
}

func sameAsset(left, right string) bool {
	return strings.EqualFold(left, right)
}

func timestampsClose(left, right time.Time, window time.Duration) bool {
	diff := left.Sub(right)
	if diff < 0 {
		diff = -diff
	}
	return diff <= window
}

func hasRawTypePrefix(rawType *string, prefix string) bool {
	if rawType == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(*rawType)), strings.ToLower(prefix))
}

func isPositiveDecimal(value string) bool {
	r, ok := new(big.Rat).SetString(value)
	return ok && r.Sign() > 0
}

// amountsClose checks if two decimal string amounts are within the given
// tolerance ratio of each other.
func amountsClose(a, b string, tolerance float64) bool {
	ra, ok1 := new(big.Rat).SetString(a)
	rb, ok2 := new(big.Rat).SetString(b)
	if !ok1 || !ok2 {
		return false
	}

	if ra.Sign() == 0 && rb.Sign() == 0 {
		return true
	}

	diff := new(big.Rat).Sub(ra, rb)
	diff.Abs(diff)

	maxVal := new(big.Rat).Set(ra)
	if rb.Cmp(ra) > 0 {
		maxVal.Set(rb)
	}

	ratio := new(big.Rat).Quo(diff, maxVal)
	threshold := new(big.Rat).SetFloat64(tolerance)

	return ratio.Cmp(threshold) <= 0
}
