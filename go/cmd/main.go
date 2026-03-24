package main

import (
	"encoding/json"
	"fmt"
	"io"
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

const payloadVersion = "1.0.0"

type normalizeOptions struct {
	ethWallets   []string
	solWallets   []string
	hlWallets    []string
	robinhoodCSV string
	etherscanKey string
	heliusKey    string
}

type runOptions struct {
	normalizeOptions
	coreCmd    string
	outputFile string
	dryRun     bool
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "cryptotax",
		Short:        "Crypto tax report generator",
		Long:         "Fetches transaction history from multiple chains, normalizes it, and runs it through the Haskell financial core to produce IRS Form 8949 reports.",
		SilenceUsage: true,
	}

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newAuditCmd())
	return rootCmd
}

func newRunCmd() *cobra.Command {
	opts := &runOptions{}

	runCmd := &cobra.Command{
		Use:          "run",
		Short:        "Fetch all transactions and generate tax report",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildPayload(opts.normalizeOptions, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			payloadJSON, err := marshalPayload(payload)
			if err != nil {
				return err
			}

			if opts.dryRun {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), string(payloadJSON))
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Sending %d transactions to Haskell core...\n", len(payload.Transactions))

			haskellCmd := exec.CommandContext(cmd.Context(), opts.coreCmd, "--output", opts.outputFile)
			haskellCmd.Stdin = strings.NewReader(string(payloadJSON))
			haskellCmd.Stdout = cmd.OutOrStdout()
			haskellCmd.Stderr = cmd.ErrOrStderr()

			if err := haskellCmd.Run(); err != nil {
				return fmt.Errorf("haskell core failed: %w", err)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Report written to %s\n", opts.outputFile)
			return nil
		},
	}

	bindNormalizeFlags(runCmd, &opts.normalizeOptions)
	runCmd.Flags().StringVar(&opts.coreCmd, "core", "cryptotax-core", "Path to Haskell core binary")
	runCmd.Flags().StringVar(&opts.outputFile, "output", "8949_report.csv", "Output file path")
	runCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print normalized JSON instead of running the core")

	return runCmd
}

func bindNormalizeFlags(cmd *cobra.Command, opts *normalizeOptions) {
	cmd.Flags().StringSliceVar(&opts.ethWallets, "eth-wallet", nil, "Ethereum/Arbitrum wallet address(es)")
	cmd.Flags().StringSliceVar(&opts.solWallets, "sol-wallet", nil, "Solana wallet address(es)")
	cmd.Flags().StringSliceVar(&opts.hlWallets, "hl-wallet", nil, "Hyperliquid wallet address(es)")
	cmd.Flags().StringVar(&opts.robinhoodCSV, "robinhood-csv", "", "Path to Robinhood 1099 CSV export")
	cmd.Flags().StringVar(&opts.etherscanKey, "etherscan-key", "", "Etherscan API key (or ETHERSCAN_API_KEY env)")
	cmd.Flags().StringVar(&opts.heliusKey, "helius-key", "", "Helius API key (or HELIUS_API_KEY env)")
}

func buildPayload(opts normalizeOptions, stderr io.Writer) (types.TxPayload, error) {
	var allRaw []fetcher.RawTransaction
	var wallets []string
	walletSet := make(map[string]struct{})

	opts.etherscanKey = envOrValue(opts.etherscanKey, "ETHERSCAN_API_KEY")
	opts.heliusKey = envOrValue(opts.heliusKey, "HELIUS_API_KEY")

	if len(opts.ethWallets) == 0 && len(opts.solWallets) == 0 && len(opts.hlWallets) == 0 && opts.robinhoodCSV == "" {
		return types.TxPayload{}, fmt.Errorf("no input source specified: provide at least one wallet flag or --robinhood-csv")
	}
	if len(opts.ethWallets) > 0 && opts.etherscanKey == "" {
		return types.TxPayload{}, fmt.Errorf("etherscan API key required: pass --etherscan-key or set ETHERSCAN_API_KEY")
	}
	if len(opts.solWallets) > 0 && opts.heliusKey == "" {
		return types.TxPayload{}, fmt.Errorf("helius API key required: pass --helius-key or set HELIUS_API_KEY")
	}

	for _, ethWallet := range opts.ethWallets {
		ethWallet = strings.TrimSpace(ethWallet)
		if ethWallet == "" {
			continue
		}
		wallets = appendUniqueWallet(wallets, walletSet, ethWallet)

		ethFetcher := fetcher.NewEtherscan(opts.etherscanKey, 1, types.ChainEthereum)
		fmt.Fprintf(stderr, "Fetching %s for %s...\n", ethFetcher.Name(), ethWallet)
		ethTxs, err := ethFetcher.Fetch(ethWallet)
		if err != nil {
			return types.TxPayload{}, fmt.Errorf("ethereum fetch: %w", err)
		}
		allRaw = append(allRaw, ethTxs...)

		arbFetcher := fetcher.NewEtherscan(opts.etherscanKey, 42161, types.ChainArbitrum)
		fmt.Fprintf(stderr, "Fetching %s for %s...\n", arbFetcher.Name(), ethWallet)
		arbTxs, err := arbFetcher.Fetch(ethWallet)
		if err != nil {
			return types.TxPayload{}, fmt.Errorf("arbitrum fetch: %w", err)
		}
		allRaw = append(allRaw, arbTxs...)
	}

	for _, solWallet := range opts.solWallets {
		solWallet = strings.TrimSpace(solWallet)
		if solWallet == "" {
			continue
		}
		wallets = appendUniqueWallet(wallets, walletSet, solWallet)

		hFetcher := fetcher.NewHelius(opts.heliusKey)
		fmt.Fprintf(stderr, "Fetching %s for %s...\n", hFetcher.Name(), solWallet)
		solTxs, err := hFetcher.Fetch(solWallet)
		if err != nil {
			return types.TxPayload{}, fmt.Errorf("solana fetch: %w", err)
		}
		allRaw = append(allRaw, solTxs...)
	}

	for _, hlWallet := range opts.hlWallets {
		hlWallet = strings.TrimSpace(hlWallet)
		if hlWallet == "" {
			continue
		}
		wallets = appendUniqueWallet(wallets, walletSet, hlWallet)

		hlFetcher := fetcher.NewHyperliquid()
		fmt.Fprintf(stderr, "Fetching %s for %s...\n", hlFetcher.Name(), hlWallet)
		hlTxs, err := hlFetcher.Fetch(hlWallet)
		if err != nil {
			return types.TxPayload{}, fmt.Errorf("hyperliquid fetch: %w", err)
		}
		allRaw = append(allRaw, hlTxs...)
	}

	if opts.robinhoodCSV != "" {
		wallets = appendUniqueWallet(wallets, walletSet, "robinhood")

		rhFetcher := fetcher.NewRobinhood(opts.robinhoodCSV)
		fmt.Fprintf(stderr, "Fetching %s...\n", rhFetcher.Name())
		rhTxs, err := rhFetcher.Fetch("")
		if err != nil {
			return types.TxPayload{}, fmt.Errorf("robinhood parse: %w", err)
		}
		allRaw = append(allRaw, rhTxs...)
	}

	fmt.Fprintf(stderr, "Fetched %d raw transactions total\n", len(allRaw))

	pp := price.NewProvider()
	normalized, err := normalize.Normalize(allRaw, wallets, pp)
	if err != nil {
		return types.TxPayload{}, fmt.Errorf("normalization: %w", err)
	}

	normalized = transfer.MatchTransfers(normalized, wallets)

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Timestamp.Before(normalized[j].Timestamp)
	})

	return types.TxPayload{
		Version:      payloadVersion,
		Wallets:      wallets,
		Transactions: normalized,
	}, nil
}

func marshalPayload(payload types.TxPayload) ([]byte, error) {
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling payload: %w", err)
	}
	return payloadJSON, nil
}

func envOrValue(value, envKey string) string {
	if value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(envKey))
}

func appendUniqueWallet(wallets []string, seen map[string]struct{}, wallet string) []string {
	wallet = strings.TrimSpace(wallet)
	if wallet == "" {
		return wallets
	}

	key := strings.ToLower(wallet)
	if _, ok := seen[key]; ok {
		return wallets
	}

	seen[key] = struct{}{}
	return append(wallets, wallet)
}
