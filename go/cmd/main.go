package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/kevin/cryptotax/fetcher"
	"github.com/kevin/cryptotax/normalize"
	"github.com/kevin/cryptotax/price"
	"github.com/kevin/cryptotax/transfer"
	"github.com/kevin/cryptotax/types"
	"github.com/spf13/cobra"
)

func main() {
	var (
		ethWallets   []string
		solWallets   []string
		hlWallets    []string
		robinhoodCSV string
		etherscanKey string
		heliusKey    string
		coreCmd      string
		outputFile   string
		dryRun       bool
	)

	rootCmd := &cobra.Command{
		Use:   "cryptotax",
		Short: "Crypto tax report generator",
		Long:  "Fetches transaction history from multiple chains, normalizes it, and runs it through the Haskell financial core to produce IRS Form 8949 reports.",
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Fetch all transactions and generate tax report",
		RunE: func(cmd *cobra.Command, args []string) error {
			var allRaw []fetcher.RawTransaction
			var wallets []string

			// --- Etherscan (Ethereum + Arbitrum) ---
			for _, ethWallet := range ethWallets {
				wallets = append(wallets, ethWallet)

				ethFetcher := fetcher.NewEtherscan(etherscanKey, 1, types.ChainEthereum)
				fmt.Fprintf(os.Stderr, "Fetching %s for %s...\n", ethFetcher.Name(), ethWallet)
				ethTxs, err := ethFetcher.Fetch(ethWallet)
				if err != nil {
					return fmt.Errorf("ethereum fetch: %w", err)
				}
				allRaw = append(allRaw, ethTxs...)

				arbFetcher := fetcher.NewEtherscan(etherscanKey, 42161, types.ChainArbitrum)
				fmt.Fprintf(os.Stderr, "Fetching %s for %s...\n", arbFetcher.Name(), ethWallet)
				arbTxs, err := arbFetcher.Fetch(ethWallet)
				if err != nil {
					return fmt.Errorf("arbitrum fetch: %w", err)
				}
				allRaw = append(allRaw, arbTxs...)
			}

			// --- Helius (Solana) ---
			for _, solWallet := range solWallets {
				wallets = append(wallets, solWallet)

				hFetcher := fetcher.NewHelius(heliusKey)
				fmt.Fprintf(os.Stderr, "Fetching %s for %s...\n", hFetcher.Name(), solWallet)
				solTxs, err := hFetcher.Fetch(solWallet)
				if err != nil {
					return fmt.Errorf("solana fetch: %w", err)
				}
				allRaw = append(allRaw, solTxs...)
			}

			// --- Hyperliquid ---
			for _, hlWallet := range hlWallets {
				wallets = append(wallets, hlWallet)

				hlFetcher := fetcher.NewHyperliquid()
				fmt.Fprintf(os.Stderr, "Fetching %s for %s...\n", hlFetcher.Name(), hlWallet)
				hlTxs, err := hlFetcher.Fetch(hlWallet)
				if err != nil {
					return fmt.Errorf("hyperliquid fetch: %w", err)
				}
				allRaw = append(allRaw, hlTxs...)
			}

			// --- Robinhood CSV ---
			if robinhoodCSV != "" {
				wallets = append(wallets, "robinhood")

				rhFetcher := fetcher.NewRobinhood(robinhoodCSV)
				fmt.Fprintf(os.Stderr, "Fetching %s...\n", rhFetcher.Name())
				rhTxs, err := rhFetcher.Fetch("")
				if err != nil {
					return fmt.Errorf("robinhood parse: %w", err)
				}
				allRaw = append(allRaw, rhTxs...)
			}

			fmt.Fprintf(os.Stderr, "Fetched %d raw transactions total\n", len(allRaw))

			// Normalize raw transactions
			pp := price.NewProvider()
			normalized, err := normalize.Normalize(allRaw, wallets, pp)
			if err != nil {
				return fmt.Errorf("normalization: %w", err)
			}

			// Run transfer matching
			normalized = transfer.MatchTransfers(normalized, wallets)

			// Sort by timestamp
			sort.Slice(normalized, func(i, j int) bool {
				return normalized[i].Timestamp.Before(normalized[j].Timestamp)
			})

			payload := types.TxPayload{
				Version:      "1.0.0",
				Wallets:      wallets,
				Transactions: normalized,
			}

			payloadJSON, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling payload: %w", err)
			}

			// --- Dry run: just dump the JSON ---
			if dryRun {
				fmt.Println(string(payloadJSON))
				return nil
			}

			// --- Pipe to Haskell core ---
			fmt.Fprintf(os.Stderr, "Sending %d transactions to Haskell core...\n", len(normalized))

			haskellCmd := exec.Command(coreCmd, "--output", outputFile)
			haskellCmd.Stdin = strings.NewReader(string(payloadJSON))
			haskellCmd.Stdout = os.Stdout
			haskellCmd.Stderr = os.Stderr

			if err := haskellCmd.Run(); err != nil {
				return fmt.Errorf("haskell core failed: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Report written to %s\n", outputFile)
			return nil
		},
	}

	// Multi-wallet flags (comma-separated or repeated)
	runCmd.Flags().StringSliceVar(&ethWallets, "eth-wallet", nil, "Ethereum/Arbitrum wallet address(es)")
	runCmd.Flags().StringSliceVar(&solWallets, "sol-wallet", nil, "Solana wallet address(es)")
	runCmd.Flags().StringSliceVar(&hlWallets, "hl-wallet", nil, "Hyperliquid wallet address(es)")
	runCmd.Flags().StringVar(&robinhoodCSV, "robinhood-csv", "", "Path to Robinhood 1099 CSV export")

	// API key flags
	runCmd.Flags().StringVar(&etherscanKey, "etherscan-key", os.Getenv("ETHERSCAN_API_KEY"), "Etherscan API key (or ETHERSCAN_API_KEY env)")
	runCmd.Flags().StringVar(&heliusKey, "helius-key", os.Getenv("HELIUS_API_KEY"), "Helius API key (or HELIUS_API_KEY env)")

	// Output flags
	runCmd.Flags().StringVar(&coreCmd, "core", "cryptotax-core", "Path to Haskell core binary")
	runCmd.Flags().StringVar(&outputFile, "output", "8949_report.csv", "Output file path")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print normalized JSON instead of running the core")

	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
