package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kevin/cryptotax/normalize"
	"github.com/kevin/cryptotax/types"
)

func TestFilterPayloadAppliesCombinedFilters(t *testing.T) {
	t.Parallel()

	payload := testPayload()
	from := payload.Transactions[0].Timestamp.Add(-time.Minute)
	to := payload.Transactions[0].Timestamp.Add(time.Minute)

	filtered := FilterPayload(payload, Filters{
		TxID:          "tx-1",
		FromTimestamp: &from,
		ToTimestamp:   &to,
		Wallet:        "0xaaa",
		Asset:         "eth",
		RawType:       "native transfer",
	})

	if len(filtered.Transactions) != 1 {
		t.Fatalf("expected 1 transaction after filtering, got %d", len(filtered.Transactions))
	}

	tx := filtered.Transactions[0]
	if tx.ID != "tx-1" || tx.Wallet != "0xaaa" {
		t.Fatalf("unexpected transaction after filtering: %#v", tx)
	}

	if len(filtered.Wallets) != len(payload.Wallets) {
		t.Fatalf("expected wallets to be preserved, got %v", filtered.Wallets)
	}
}

func TestFilterPayloadMatchesFeeAssetCaseInsensitive(t *testing.T) {
	t.Parallel()

	payload := testPayload()
	filtered := FilterPayload(payload, Filters{Asset: "eth"})

	if len(filtered.Transactions) != 2 {
		t.Fatalf("expected 2 ETH transactions, got %d", len(filtered.Transactions))
	}
}

func TestFilterPayloadMatchesCanonicalAsset(t *testing.T) {
	t.Parallel()

	canonical := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	payload := testPayload()
	payload.Transactions[2].Received.AssetCanonical = &canonical

	filtered := FilterPayload(payload, Filters{Asset: canonical})

	if len(filtered.Transactions) != 1 {
		t.Fatalf("expected 1 canonical asset match, got %d", len(filtered.Transactions))
	}
	if filtered.Transactions[0].ID != "tx-1" || filtered.Transactions[0].Wallet != "0xbbb" {
		t.Fatalf("unexpected transaction after canonical asset filtering: %#v", filtered.Transactions[0])
	}
}

func TestBuildSummaryAggregatesExactAssetTotals(t *testing.T) {
	t.Parallel()

	summary, err := BuildSummary(testPayload())
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}

	if summary.TransactionCount != 3 {
		t.Fatalf("expected 3 transactions, got %d", summary.TransactionCount)
	}
	if summary.UniqueIDCount != 2 {
		t.Fatalf("expected 2 unique ids, got %d", summary.UniqueIDCount)
	}
	if summary.MissingRawTypeCount != 1 {
		t.Fatalf("expected 1 missing raw_type, got %d", summary.MissingRawTypeCount)
	}
	if summary.TimestampRange == nil {
		t.Fatalf("expected timestamp range")
	}
	if got := summary.ByTxType[types.TxTransferOut]; got != 1 {
		t.Fatalf("expected 1 transfer_out, got %d", got)
	}
	if got := summary.BySource[types.SourceEtherscan]; got != 2 {
		t.Fatalf("expected 2 etherscan rows, got %d", got)
	}
	if got := summary.BySource[types.SourceHelius]; got != 1 {
		t.Fatalf("expected 1 helius row, got %d", got)
	}
	if got := summary.ByWallet["0xbbb"]; got != 2 {
		t.Fatalf("expected wallet 0xbbb count 2, got %d", got)
	}
	if got := summary.ByRawType["native transfer"]; got != 1 {
		t.Fatalf("expected raw_type count 1, got %d", got)
	}

	eth := summary.ByAsset["ETH"]
	if eth.TransactionCount != 2 {
		t.Fatalf("expected ETH transaction count 2, got %d", eth.TransactionCount)
	}
	if eth.SentAmount != "1.25" {
		t.Fatalf("expected ETH sent amount 1.25, got %q", eth.SentAmount)
	}
	if eth.ReceivedAmount != "0.75" {
		t.Fatalf("expected ETH received amount 0.75, got %q", eth.ReceivedAmount)
	}
	if eth.FeeAmount != "0.01" {
		t.Fatalf("expected ETH fee amount 0.01, got %q", eth.FeeAmount)
	}

	usdc := summary.ByAsset["USDC"]
	if usdc.TransactionCount != 1 {
		t.Fatalf("expected USDC transaction count 1, got %d", usdc.TransactionCount)
	}
	if usdc.ReceivedAmount != "25" {
		t.Fatalf("expected USDC received amount 25, got %q", usdc.ReceivedAmount)
	}

	if len(summary.ZeroUSDValueRows) != 1 {
		t.Fatalf("expected 1 zero usd_value row, got %d", len(summary.ZeroUSDValueRows))
	}
	zeroRow := summary.ZeroUSDValueRows[0]
	if zeroRow.ID != "tx-2" {
		t.Fatalf("expected zero usd_value row tx-2, got %q", zeroRow.ID)
	}
	if !slices.Equal(zeroRow.Fields, []string{"received"}) {
		t.Fatalf("unexpected zero usd_value fields: %v", zeroRow.Fields)
	}
	if !slices.Equal(zeroRow.Assets, []string{"eth"}) {
		t.Fatalf("unexpected zero usd_value assets: %v", zeroRow.Assets)
	}

	if len(summary.SuspiciousAssetRows) != 1 {
		t.Fatalf("expected 1 suspicious asset row, got %d", len(summary.SuspiciousAssetRows))
	}
	suspiciousRow := summary.SuspiciousAssetRows[0]
	if suspiciousRow.ID != "tx-2" {
		t.Fatalf("expected suspicious asset row tx-2, got %q", suspiciousRow.ID)
	}
	if !slices.Equal(suspiciousRow.Assets, []string{"eth"}) {
		t.Fatalf("unexpected suspicious assets: %v", suspiciousRow.Assets)
	}
	if !slices.Equal(suspiciousRow.Fields, []string{"received"}) {
		t.Fatalf("unexpected suspicious asset fields: %v", suspiciousRow.Fields)
	}
	if !slices.Equal(suspiciousRow.Reasons, []string{"non_canonical_asset_case"}) {
		t.Fatalf("unexpected suspicious asset reasons: %v", suspiciousRow.Reasons)
	}
}

func TestBuildSummaryFlagsAddressLikeAndPlaceholderAssets(t *testing.T) {
	t.Parallel()

	rawType := "contract interaction"
	summary, err := BuildSummary(types.TxPayload{
		Version: "1.0.0",
		Wallets: []string{"0xaaa"},
		Transactions: []types.Transaction{
			{
				ID:        "tx-address-like",
				Timestamp: time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC),
				Source:    types.SourceEtherscan,
				Chain:     types.ChainEthereum,
				TxType:    types.TxSwap,
				Wallet:    "0xaaa",
				Sent: &types.AssetAmount{
					Asset:    "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
					Amount:   "10",
					USDValue: "10",
				},
				Received: &types.AssetAmount{
					Asset:    "UNKNOWN",
					Amount:   "0.01",
					USDValue: "25",
				},
				RawType: &rawType,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}

	if len(summary.SuspiciousAssetRows) != 1 {
		t.Fatalf("expected 1 suspicious asset row, got %d", len(summary.SuspiciousAssetRows))
	}

	row := summary.SuspiciousAssetRows[0]
	if !slices.Equal(row.Assets, []string{
		"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
		"UNKNOWN",
	}) {
		t.Fatalf("unexpected suspicious assets: %v", row.Assets)
	}
	if !slices.Equal(row.Fields, []string{
		"sent",
		"received",
	}) {
		t.Fatalf("unexpected suspicious asset fields: %v", row.Fields)
	}
	if !slices.Equal(row.Reasons, []string{
		"address_like_asset_symbol",
		"placeholder_asset_symbol",
	}) {
		t.Fatalf("unexpected suspicious asset reasons: %v", row.Reasons)
	}
}

func TestBuildSummaryFlagsUnresolvedValuationForSuspiciousSolanaSwapLeg(t *testing.T) {
	t.Parallel()

	payload, err := LoadPayload(filepath.Join("testdata", "real-wallet", "sol-pumpfun-zero-usd.input.json"))
	if err != nil {
		t.Fatalf("LoadPayload returned error: %v", err)
	}

	summary, err := BuildSummary(payload)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}

	if len(summary.ZeroUSDValueRows) != 1 {
		t.Fatalf("expected 1 zero usd_value row, got %d", len(summary.ZeroUSDValueRows))
	}
	zeroRow := summary.ZeroUSDValueRows[0]
	if zeroRow.ID != "PbxPFcX7JQF6PuTMjRs2xKuC2azpAmnc1uALwYxCKuwnWFT3vb156p17CZRYjcU1ySB1ANHSWgGfPEfHtE2QXKm" {
		t.Fatalf("unexpected zero usd_value row id: %q", zeroRow.ID)
	}
	if !slices.Equal(zeroRow.Fields, []string{"received"}) {
		t.Fatalf("unexpected zero usd_value fields: %v", zeroRow.Fields)
	}
	if !slices.Equal(zeroRow.Assets, []string{"CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP"}) {
		t.Fatalf("unexpected zero usd_value assets: %v", zeroRow.Assets)
	}

	if len(summary.SuspiciousAssetRows) != 1 {
		t.Fatalf("expected 1 suspicious asset row, got %d", len(summary.SuspiciousAssetRows))
	}
	row := summary.SuspiciousAssetRows[0]
	if row.ID != zeroRow.ID {
		t.Fatalf("expected suspicious asset row id %q, got %q", zeroRow.ID, row.ID)
	}
	if !slices.Equal(row.Assets, zeroRow.Assets) {
		t.Fatalf("unexpected suspicious assets: %v", row.Assets)
	}
	if !slices.Equal(row.Fields, []string{"received"}) {
		t.Fatalf("unexpected suspicious asset fields: %v", row.Fields)
	}
	if !slices.Equal(row.Reasons, []string{
		"address_like_asset_symbol",
		"unresolved_asset_valuation",
	}) {
		t.Fatalf("unexpected suspicious asset reasons: %v", row.Reasons)
	}
}

func TestWriteCaptureArtifactsWritesPayloadAndCommandFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "audit", "normalized.json")
	payload := testPayload()
	commandLine := "nix run .#audit -- capture audit/normalized.json --eth-wallet 0xaaa"

	if err := WriteCaptureArtifacts(outputPath, payload, commandLine, nil); err != nil {
		t.Fatalf("WriteCaptureArtifacts returned error: %v", err)
	}

	writtenPayload, err := LoadPayload(outputPath)
	if err != nil {
		t.Fatalf("LoadPayload returned error: %v", err)
	}
	if len(writtenPayload.Transactions) != len(payload.Transactions) {
		t.Fatalf("expected %d transactions, got %d", len(payload.Transactions), len(writtenPayload.Transactions))
	}

	commandBytes, err := os.ReadFile(outputPath + ".command")
	if err != nil {
		t.Fatalf("read command file: %v", err)
	}

	expectedCommand := "# Re-run command\n" + commandLine + "\n"
	if string(commandBytes) != expectedCommand {
		t.Fatalf("unexpected command file contents:\n%s", string(commandBytes))
	}

	// No skipped rows sidecar when none provided
	if _, err := os.Stat(outputPath + ".skipped.json"); err == nil {
		t.Fatal("expected no skipped rows file when skipped is nil")
	}
}

func TestWriteCaptureArtifactsWritesSkippedRowsSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "audit", "normalized.json")
	payload := testPayload()
	commandLine := "nix run .#audit -- capture audit/normalized.json --eth-wallet 0xaaa"
	skipped := []normalize.SkippedRow{
		{
			TxID:    "bad-tx-1",
			Source:  types.SourceEtherscan,
			Chain:   types.ChainEthereum,
			RawType: "token transfer",
			Reason:  "missing wallet on raw transaction",
		},
		{
			TxID:   "bad-tx-2",
			Source: types.SourceHelius,
			Chain:  types.ChainSolana,
			Reason: "missing wallet on raw transaction",
		},
	}

	if err := WriteCaptureArtifacts(outputPath, payload, commandLine, skipped); err != nil {
		t.Fatalf("WriteCaptureArtifacts returned error: %v", err)
	}

	// Payload and command still written
	if _, err := LoadPayload(outputPath); err != nil {
		t.Fatalf("LoadPayload returned error: %v", err)
	}
	if _, err := os.ReadFile(outputPath + ".command"); err != nil {
		t.Fatalf("read command file: %v", err)
	}

	// Skipped rows sidecar is machine-readable JSON
	skippedBytes, err := os.ReadFile(outputPath + ".skipped.json")
	if err != nil {
		t.Fatalf("read skipped rows file: %v", err)
	}

	var loaded []normalize.SkippedRow
	if err := json.Unmarshal(skippedBytes, &loaded); err != nil {
		t.Fatalf("unmarshal skipped rows: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 skipped rows, got %d", len(loaded))
	}
	if loaded[0].TxID != "bad-tx-1" || loaded[0].Source != types.SourceEtherscan || loaded[0].Reason != "missing wallet on raw transaction" {
		t.Fatalf("unexpected first skipped row: %#v", loaded[0])
	}
	if loaded[1].TxID != "bad-tx-2" || loaded[1].RawType != "" {
		t.Fatalf("unexpected second skipped row: %#v", loaded[1])
	}
}

func TestWriteCaptureArtifactsRemovesStaleSkippedRowsSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "audit", "normalized.json")
	payload := testPayload()
	commandLine := "nix run .#audit -- capture audit/normalized.json --eth-wallet 0xaaa"
	skipped := []normalize.SkippedRow{{
		TxID:   "bad-tx",
		Source: types.SourceHelius,
		Chain:  types.ChainSolana,
		Reason: "missing wallet on raw transaction",
	}}

	if err := WriteCaptureArtifacts(outputPath, payload, commandLine, skipped); err != nil {
		t.Fatalf("first WriteCaptureArtifacts returned error: %v", err)
	}
	if _, err := os.Stat(outputPath + ".skipped.json"); err != nil {
		t.Fatalf("expected skipped rows sidecar after first capture: %v", err)
	}

	if err := WriteCaptureArtifacts(outputPath, payload, commandLine, nil); err != nil {
		t.Fatalf("second WriteCaptureArtifacts returned error: %v", err)
	}
	if _, err := os.Stat(outputPath + ".skipped.json"); !os.IsNotExist(err) {
		t.Fatalf("expected stale skipped rows sidecar to be removed, stat error: %v", err)
	}
}

func testPayload() types.TxPayload {
	firstRawType := "native transfer"
	secondRawType := "token transfer"
	start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	return types.TxPayload{
		Version: "1.0.0",
		Wallets: []string{"0xaaa", "0xbbb"},
		Transactions: []types.Transaction{
			{
				ID:        "tx-1",
				Timestamp: start,
				Source:    types.SourceEtherscan,
				Chain:     types.ChainEthereum,
				TxType:    types.TxTransferOut,
				Wallet:    "0xaaa",
				Sent: &types.AssetAmount{
					Asset:    "ETH",
					Amount:   "1.25",
					USDValue: "2500",
				},
				Fee: &types.AssetAmount{
					Asset:    "ETH",
					Amount:   "0.010",
					USDValue: "20",
				},
				RawType: &firstRawType,
			},
			{
				ID:        "tx-2",
				Timestamp: start.Add(2 * time.Hour),
				Source:    types.SourceHelius,
				Chain:     types.ChainSolana,
				TxType:    types.TxTransferIn,
				Wallet:    "0xbbb",
				Received: &types.AssetAmount{
					Asset:    "eth",
					Amount:   "0.75",
					USDValue: "0",
				},
			},
			{
				ID:        "tx-1",
				Timestamp: start.Add(3 * time.Hour),
				Source:    types.SourceEtherscan,
				Chain:     types.ChainEthereum,
				TxType:    types.TxTransferIn,
				Wallet:    "0xbbb",
				Received: &types.AssetAmount{
					Asset:    "USDC",
					Amount:   "25.00",
					USDValue: "25.00",
				},
				RawType: &secondRawType,
			},
		},
	}
}

// Regression: integer totals were corrupted by trailing-zero trimming
// ("1000" rendered as "1"), and exponent-notation inputs collapsed to "0".
func TestSummaryAmountTotalsExact(t *testing.T) {
	t.Parallel()

	payload := types.TxPayload{
		Version: "1.0.0",
		Wallets: []string{"w"},
		Transactions: []types.Transaction{
			{
				ID: "t1", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Source: types.SourceRobinhood, Chain: types.ChainRobinhood,
				TxType: types.TxBuy, Wallet: "w",
				Received: &types.AssetAmount{Asset: "USDC", Amount: "1000", USDValue: "1000"},
			},
			{
				ID: "t2", Timestamp: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
				Source: types.SourceRobinhood, Chain: types.ChainRobinhood,
				TxType: types.TxSell, Wallet: "w",
				Sent: &types.AssetAmount{Asset: "SOL", Amount: "1e-05", USDValue: "0"},
			},
		},
	}

	summary, err := BuildSummary(payload)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if got := summary.ByAsset["USDC"].ReceivedAmount; got != "1000" {
		t.Errorf("USDC received total = %q, want 1000", got)
	}
	if got := summary.ByAsset["SOL"].SentAmount; got != "0.00001" {
		t.Errorf("SOL sent total = %q, want 0.00001", got)
	}
}

// One wallet captured under two casings must aggregate and filter as one
// identity (EVM addresses are case-insensitive; Solana stays exact).
func TestWalletIdentityFoldsEVMCase(t *testing.T) {
	t.Parallel()

	mixed := "0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA"
	lower := "0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca"
	payload := types.TxPayload{
		Version: "1.0.0",
		Wallets: []string{mixed},
		Transactions: []types.Transaction{
			{ID: "a", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Source: types.SourceEtherscan, Chain: types.ChainEthereum, TxType: types.TxTransferIn, Wallet: mixed},
			{ID: "b", Timestamp: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Source: types.SourceEtherscan, Chain: types.ChainEthereum, TxType: types.TxTransferIn, Wallet: lower},
		},
	}

	summary, err := BuildSummary(payload)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if got := summary.ByWallet[lower]; got != 2 {
		t.Errorf("ByWallet[%q] = %d, want 2 (case-folded)", lower, got)
	}

	filtered := FilterPayload(payload, Filters{Wallet: mixed})
	if len(filtered.Transactions) != 2 {
		t.Errorf("wallet filter matched %d rows, want 2", len(filtered.Transactions))
	}
}
