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
//  4. One is transfer_out/sell, the other is transfer_in/income
func MatchTransfers(txs []types.Transaction, wallets []string) []types.Transaction {
	matched := make(map[int]bool)

	for i := range txs {
		if txs[i].TxType != types.TxTransferOut && txs[i].TxType != types.TxSell {
			continue
		}
		if txs[i].Sent == nil {
			continue
		}

		for j := range txs {
			if i == j || matched[j] {
				continue
			}

			if txs[j].TxType != types.TxIncome && txs[j].TxType != types.TxTransferIn {
				continue
			}
			if txs[j].Received == nil {
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

			txs[i].TxType = types.TxTransferOut
			txs[j].TxType = types.TxTransferIn
			matched[i] = true
			matched[j] = true
			break
		}
	}

	return txs
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
