package fetcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kevin/cryptotax/decimal"
	"github.com/kevin/cryptotax/types"
)

// Helius fetches Solana transaction history via the Helius API.
// Uses getSignaturesForAddress (RPC) + Enhanced Transactions API (batch parse).
//
// Signatures are collected for the owner wallet AND each of its token
// accounts: transactions that only touch an existing associated token
// account (e.g. an inbound USDC transfer) do not reference the owner
// address at all and would otherwise be invisible.
type Helius struct {
	APIKey            string
	Client            *http.Client
	RPCURL            string
	EnhancedURL       string
	LegacyEnhancedURL string
}

const (
	heliusSignatureLimit = 1000
	heliusBatchSize      = 100
	heliusRateLimitDelay = 150 * time.Millisecond // ~7 req/s, well under 10 req/s

	// wrappedSOLMint is economically identical to native SOL; swap netting
	// treats the two as one asset so wrap/unwrap legs cancel out.
	wrappedSOLMint = "So11111111111111111111111111111111111111112"

	splTokenProgram     = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	splToken2022Program = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
)

func NewHelius(apiKey string) *Helius {
	return &Helius{
		APIKey:            apiKey,
		Client:            &http.Client{Timeout: 30 * time.Second},
		RPCURL:            "https://mainnet.helius-rpc.com/",
		EnhancedURL:       "https://api-mainnet.helius-rpc.com/v0/transactions",
		LegacyEnhancedURL: "https://api.helius.xyz/v0/transactions",
	}
}

func (h *Helius) Name() string { return "helius/solana" }

func (h *Helius) Fetch(wallet string) ([]RawTransaction, error) {
	// Step 1: collect signatures for the owner and all its token accounts.
	addresses := []string{wallet}
	tokenAccounts, err := h.getTokenAccounts(wallet)
	if err != nil {
		return nil, fmt.Errorf("listing token accounts: %w", err)
	}
	addresses = append(addresses, tokenAccounts...)

	var sigs []string
	seen := make(map[string]struct{})
	for _, addr := range addresses {
		addrSigs, err := h.getSignatures(addr)
		if err != nil {
			return nil, fmt.Errorf("getting signatures for %s: %w", addr, err)
		}
		for _, sig := range addrSigs {
			if _, ok := seen[sig]; ok {
				continue
			}
			seen[sig] = struct{}{}
			sigs = append(sigs, sig)
		}
	}

	if len(sigs) == 0 {
		return nil, nil
	}

	// Step 2: Parse via Enhanced Transactions API in batches of 100
	var allTxs []RawTransaction
	for i := 0; i < len(sigs); i += heliusBatchSize {
		end := i + heliusBatchSize
		if end > len(sigs) {
			end = len(sigs)
		}
		batch := sigs[i:end]

		txs, err := h.parseEnhanced(batch, wallet)
		if err != nil {
			return nil, fmt.Errorf("parsing batch %d: %w", i/heliusBatchSize, err)
		}
		allTxs = append(allTxs, txs...)

		time.Sleep(heliusRateLimitDelay)
	}

	return allTxs, nil
}

// getTokenAccounts lists the wallet's SPL token accounts (classic and
// Token-2022 programs).
func (h *Helius) getTokenAccounts(wallet string) ([]string, error) {
	var accounts []string
	for _, program := range []string{splTokenProgram, splToken2022Program} {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "getTokenAccountsByOwner",
			"params": []interface{}{
				wallet,
				map[string]string{"programId": program},
				map[string]string{"encoding": "jsonParsed"},
			},
		}

		respBody, err := h.post(h.rpcURL(), body)
		if err != nil {
			return nil, err
		}

		var result struct {
			Result struct {
				Value []struct {
					Pubkey string `json:"pubkey"`
				} `json:"value"`
			} `json:"result"`
			Error *heliusRPCError `json:"error"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parsing token accounts response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("getTokenAccountsByOwner RPC error %d: %s", result.Error.Code, result.Error.Message)
		}

		for _, v := range result.Result.Value {
			accounts = append(accounts, v.Pubkey)
		}
		time.Sleep(heliusRateLimitDelay)
	}
	return accounts, nil
}

func (h *Helius) getSignatures(address string) ([]string, error) {
	var allSigs []string
	var before string
	rpcURL := h.rpcURL()

	for {
		params := map[string]interface{}{
			"limit": heliusSignatureLimit,
		}
		if before != "" {
			params["before"] = before
		}

		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "getSignaturesForAddress",
			"params":  []interface{}{address, params},
		}

		respBody, err := h.post(rpcURL, body)
		if err != nil {
			return nil, fmt.Errorf("getSignaturesForAddress: %w", err)
		}

		var result struct {
			Result []struct {
				Signature string `json:"signature"`
				Err       any    `json:"err"`
			} `json:"result"`
			Error *heliusRPCError `json:"error"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parsing signatures response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("getSignaturesForAddress RPC error %d: %s", result.Error.Code, result.Error.Message)
		}

		for _, sig := range result.Result {
			// Failed transactions move no value. Their fee was still paid by
			// the fee payer, but modeling gas on failed txs is an open
			// tax-semantics question; skipping is conservative (never
			// overstates deductions). See docs/solana/QUIRKS.md.
			if sig.Err == nil {
				allSigs = append(allSigs, sig.Signature)
			}
		}

		if len(result.Result) < heliusSignatureLimit {
			break
		}
		before = result.Result[len(result.Result)-1].Signature

		time.Sleep(heliusRateLimitDelay)
	}

	return allSigs, nil
}

func (h *Helius) rpcURL() string {
	baseURL := h.RPCURL
	if baseURL == "" {
		baseURL = "https://mainnet.helius-rpc.com/"
	}
	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%sapi-key=%s", baseURL, separator, h.APIKey)
}

type heliusRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Enhanced Transactions API response types
type heliusEnhancedTx struct {
	Signature       string                 `json:"signature"`
	Type            string                 `json:"type"`
	Source          string                 `json:"source"`
	Fee             int64                  `json:"fee"`
	FeePayer        string                 `json:"feePayer"`
	Timestamp       int64                  `json:"timestamp"`
	TokenTransfers  []heliusTokenTransfer  `json:"tokenTransfers"`
	NativeTransfers []heliusNativeTransfer `json:"nativeTransfers"`
	Description     string                 `json:"description"`
}

type heliusTokenTransfer struct {
	FromUserAccount string `json:"fromUserAccount"`
	ToUserAccount   string `json:"toUserAccount"`
	Mint            string `json:"mint"`
	// Helius' structured transfer docs verify mint, amount, and accounts. If a
	// separate source-backed symbol is present in the payload, preserve it, but
	// do not depend on it for classification.
	Symbol string `json:"symbol"`
	// TokenAmount is decoded as json.Number, never float64: SPL amounts are
	// money-path values and must not pass through floating point.
	TokenAmount   json.Number `json:"tokenAmount"`
	TokenStandard string      `json:"tokenStandard"`
}

type heliusNativeTransfer struct {
	FromUserAccount string `json:"fromUserAccount"`
	ToUserAccount   string `json:"toUserAccount"`
	Amount          int64  `json:"amount"`
}

func (h *Helius) parseEnhanced(sigs []string, wallet string) ([]RawTransaction, error) {
	respBody, err := h.parseEnhancedAtURL(h.EnhancedURL, sigs)
	if err != nil && h.LegacyEnhancedURL != "" && h.LegacyEnhancedURL != h.EnhancedURL && shouldFallbackHeliusEnhanced(err) {
		respBody, err = h.parseEnhancedAtURL(h.LegacyEnhancedURL, sigs)
	}
	if err != nil {
		return nil, fmt.Errorf("enhanced transactions: %w", err)
	}

	var results []heliusEnhancedTx
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("parsing enhanced response: %w", err)
	}

	// The Enhanced API silently omits transactions it cannot parse; surface
	// the gap instead of letting history vanish without a trace.
	returned := make(map[string]struct{}, len(results))
	for _, etx := range results {
		returned[etx.Signature] = struct{}{}
	}
	var missing []string
	for _, sig := range sigs {
		if _, ok := returned[sig]; !ok {
			missing = append(missing, sig)
		}
	}
	if len(missing) > 0 {
		preview := missing
		if len(preview) > 5 {
			preview = preview[:5]
		}
		fmt.Fprintf(os.Stderr, "Warning: Helius enhanced API returned no parse for %d of %d signatures (e.g. %s) — these transactions are NOT in the report\n",
			len(missing), len(sigs), strings.Join(preview, ", "))
	}

	var txs []RawTransaction
	for _, etx := range results {
		txs = append(txs, convertHeliusTx(etx, wallet)...)
	}
	return txs, nil
}

func (h *Helius) parseEnhancedAtURL(baseURL string, sigs []string) ([]byte, error) {
	url := fmt.Sprintf("%s?api-key=%s", baseURL, h.APIKey)
	return h.post(url, map[string][]string{"transactions": sigs})
}

func shouldFallbackHeliusEnhanced(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http 530") ||
		strings.Contains(message, "error code: 1016")
}

func convertHeliusTx(etx heliusEnhancedTx, wallet string) []RawTransaction {
	feeSOL := lamportsToSOL(etx.Fee)

	switch etx.Type {
	case "SWAP":
		return convertSwap(etx, wallet, feeSOL)
	case "TRANSFER":
		return convertTransfer(etx, wallet, feeSOL)
	default:
		// Other types (NFT, COMPRESSED_NFT, etc.): extract relevant token movements
		return convertGeneric(etx, wallet, feeSOL)
	}
}

// rentDustLamportsSOL is the threshold below which a stray native-SOL flow
// alongside token swap legs is treated as account rent rather than a swap
// leg (rent deposits are refundable and routinely ride along with swaps).
var rentDustSOL = big.NewRat(1, 100) // 0.01 SOL

// convertSwap reconstructs a swap from the NET per-asset balance change of
// the wallet. Netting (rather than picking individual legs) is what the
// transfers actually did to the wallet: multi-hop routes, self-transfers
// between own token accounts, and wrap/unwrap legs all cancel out. Native
// SOL and wrapped SOL are netted as one asset.
//
// A swap is emitted only when exactly one asset was net-sent and one was
// net-received. Anything more complex falls back to per-leg preservation
// (convertGeneric) so no leg is silently dropped.
func convertSwap(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	nets := make(map[string]*big.Rat)
	symbols := make(map[string]string)

	add := func(key string, amt *big.Rat) {
		if cur, ok := nets[key]; ok {
			cur.Add(cur, amt)
		} else {
			nets[key] = new(big.Rat).Set(amt)
		}
	}

	for _, tt := range etx.TokenTransfers {
		amt, err := decimal.Parse(tt.TokenAmount.String())
		if err != nil || amt.Sign() == 0 {
			continue
		}
		key := tt.Mint
		if key == wrappedSOLMint {
			key = "SOL"
		} else if sym := strings.TrimSpace(tt.Symbol); sym != "" {
			symbols[key] = sym
		}
		if tt.FromUserAccount == wallet {
			add(key, new(big.Rat).Neg(amt))
		}
		if tt.ToUserAccount == wallet {
			add(key, amt)
		}
	}
	for _, nt := range etx.NativeTransfers {
		if nt.Amount == 0 {
			continue
		}
		amt := new(big.Rat).SetFrac(big.NewInt(nt.Amount), big.NewInt(1_000_000_000))
		if nt.FromUserAccount == wallet {
			add("SOL", new(big.Rat).Neg(amt))
		}
		if nt.ToUserAccount == wallet {
			add("SOL", amt)
		}
	}

	var sent, rcv []string
	for key, net := range nets {
		switch net.Sign() {
		case -1:
			sent = append(sent, key)
		case 1:
			rcv = append(rcv, key)
		}
	}

	// Tolerate a rent-sized native SOL flow riding along with token legs.
	if len(sent) == 2 || len(rcv) == 2 {
		if net, ok := nets["SOL"]; ok && new(big.Rat).Abs(net).Cmp(rentDustSOL) < 0 {
			sent = removeString(sent, "SOL")
			rcv = removeString(rcv, "SOL")
			delete(nets, "SOL")
		}
	}

	if len(sent) != 1 || len(rcv) != 1 {
		// Not a clean two-asset swap; preserve every leg instead of guessing.
		return convertGeneric(etx, wallet, feeSOL)
	}

	sentKey, rcvKey := sent[0], rcv[0]
	row := RawTransaction{
		ID:           etx.Signature,
		Timestamp:    etx.Timestamp,
		Source:       types.SourceHelius,
		Chain:        types.ChainSolana,
		Wallet:       wallet,
		EventGroupID: etx.Signature,
		Asset:        sentKey,
		AssetSymbol:  symbols[sentKey],
		Amount:       decimal.String(new(big.Rat).Abs(nets[sentKey])),
		Asset2:       rcvKey,
		Asset2Symbol: symbols[rcvKey],
		Amount2:      decimal.String(nets[rcvKey]),
		RawType:      fmt.Sprintf("SWAP/%s", etx.Source),
	}
	if etx.FeePayer == wallet {
		row.Fee = feeSOL
		row.FeeAsset = "SOL"
	}
	return []RawTransaction{row}
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, v := range values {
		if v != target {
			out = append(out, v)
		}
	}
	return out
}

func convertTransfer(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	return convertLegs(etx, wallet, feeSOL, "TRANSFER")
}

func convertGeneric(etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	return convertLegs(etx, wallet, feeSOL, etx.Type)
}

// convertLegs preserves each wallet-touching transfer leg as its own row.
func convertLegs(etx heliusEnhancedTx, wallet, feeSOL, rawType string) []RawTransaction {
	var txs []RawTransaction

	for _, tt := range etx.TokenTransfers {
		if !walletTouchesTransfer(wallet, tt.FromUserAccount, tt.ToUserAccount) {
			continue
		}
		if sign, err := decimal.Sign(tt.TokenAmount.String()); err == nil && sign == 0 {
			continue // zero-amount legs carry no economics
		}
		raw := RawTransaction{
			ID:           etx.Signature,
			Timestamp:    etx.Timestamp,
			Source:       types.SourceHelius,
			Chain:        types.ChainSolana,
			Wallet:       wallet,
			EventGroupID: etx.Signature,
			SplitReason:  "wallet_touching_leg_preservation",
			FromAddr:     tt.FromUserAccount,
			ToAddr:       tt.ToUserAccount,
			Asset:        tt.Mint,
			AssetSymbol:  strings.TrimSpace(tt.Symbol),
			Amount:       tt.TokenAmount.String(),
			RawType:      rawType,
		}
		txs = append(txs, raw)
	}

	for _, nt := range etx.NativeTransfers {
		if nt.Amount == 0 {
			continue
		}
		if !walletTouchesTransfer(wallet, nt.FromUserAccount, nt.ToUserAccount) {
			continue
		}
		raw := RawTransaction{
			ID:           etx.Signature,
			Timestamp:    etx.Timestamp,
			Source:       types.SourceHelius,
			Chain:        types.ChainSolana,
			Wallet:       wallet,
			EventGroupID: etx.Signature,
			SplitReason:  "wallet_touching_leg_preservation",
			FromAddr:     nt.FromUserAccount,
			ToAddr:       nt.ToUserAccount,
			Asset:        "SOL",
			Amount:       lamportsToSOL(nt.Amount),
			RawType:      rawType,
		}
		txs = append(txs, raw)
	}

	return attachFeeToPrimaryHeliusRow(txs, etx, wallet, feeSOL)
}

func (h *Helius) post(url string, body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := h.Client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		// err may embed the request URL (including the API key) via
		// url.Error, so redact before surfacing.
		return nil, fmt.Errorf("HTTP POST: %s", h.redact(err.Error()))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, h.redact(string(respBody)))
	}

	return respBody, nil
}

// redact removes the API key from text destined for errors or logs.
func (h *Helius) redact(text string) string {
	if h.APIKey == "" {
		return text
	}
	return strings.ReplaceAll(text, h.APIKey, "***")
}

// lamportsToSOL converts lamports (int64) to a SOL decimal string.
func lamportsToSOL(lamports int64) string {
	r := new(big.Rat).SetFrac(
		big.NewInt(lamports),
		new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil),
	)
	return decimal.String(r)
}

// walletTouchesTransfer compares base58 addresses exactly — base58 is
// case-sensitive, so case-folding could conflate distinct addresses.
func walletTouchesTransfer(wallet, from, to string) bool {
	return from == wallet || to == wallet
}

// attachFeeToPrimaryHeliusRow attaches the network fee to the first
// outbound row of a signature, but only when the wallet actually paid it
// (it is the fee payer). Fees on sponsored/gasless transactions belong to
// the sponsor, not the wallet.
func attachFeeToPrimaryHeliusRow(txs []RawTransaction, etx heliusEnhancedTx, wallet, feeSOL string) []RawTransaction {
	if feeSOL == "" || feeSOL == "0" || len(txs) == 0 || etx.FeePayer != wallet {
		return txs
	}

	holder := 0
	for i := range txs {
		if txs[i].FromAddr == wallet {
			holder = i
			break
		}
	}
	txs[holder].Fee = feeSOL
	txs[holder].FeeAsset = "SOL"

	return txs
}
