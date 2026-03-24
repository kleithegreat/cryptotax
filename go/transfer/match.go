package transfer

import (
	"math"
	"math/big"
	"strings"

	"github.com/kevin/cryptotax/types"
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
		if txs[i].TxType != types.TxTransferOut {
			continue
		}
		if txs[i].Sent == nil {
			continue
		}
		if !isOwnWalletRef(ownWallets, txs[i].Wallet) || !isOwnCounterparty(ownWallets, txs[i].Counterparty) {
			continue
		}

		for j := range txs {
			if i == j || matched[j] {
				continue
			}

			if txs[j].TxType != types.TxTransferIn {
				continue
			}
			if txs[j].Received == nil {
				continue
			}
			if !isOwnWalletRef(ownWallets, txs[j].Wallet) || !isOwnCounterparty(ownWallets, txs[j].Counterparty) {
				continue
			}
			if !counterpartiesMatch(txs[i], txs[j]) {
				continue
			}

			if strings.ToUpper(txs[i].Sent.Asset) != strings.ToUpper(txs[j].Received.Asset) {
				continue
			}

			if !amountsClose(txs[i].Sent.Amount, txs[j].Received.Amount, 0.01) {
				continue
			}

			diff := txs[i].Timestamp.Sub(txs[j].Timestamp)
			if math.Abs(diff.Minutes()) > 30 {
				continue
			}

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
