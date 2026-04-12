package main

import (
	"fmt"
	"os"
	"strings"

	audittool "github.com/kevin/cryptotax/audit"
	"github.com/spf13/cobra"
)

type auditFilterOptions struct {
	txID          string
	fromTimestamp string
	toTimestamp   string
	wallet        string
	asset         string
	rawType       string
}

func newAuditCmd() *cobra.Command {
	auditCmd := &cobra.Command{
		Use:          "audit",
		Short:        "Audit normalized transaction payloads",
		SilenceUsage: true,
	}

	auditCmd.AddCommand(newAuditCaptureCmd())
	auditCmd.AddCommand(newAuditFilterCmd())
	auditCmd.AddCommand(newAuditSummaryCmd())
	return auditCmd
}

func newAuditCaptureCmd() *cobra.Command {
	opts := &normalizeOptions{}

	captureCmd := &cobra.Command{
		Use:          "capture OUTPUT_JSON",
		Short:        "Fetch and write normalized JSON for audit review",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, skipped, err := buildPayload(*opts, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			outputPath := args[0]
			commandLine := captureRerunCommand()
			if err := audittool.WriteCaptureArtifacts(outputPath, payload, commandLine, skipped); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Wrote normalized JSON to %s\n", outputPath)
			fmt.Fprintf(cmd.ErrOrStderr(), "Recorded command in %s.command\n", outputPath)
			if len(skipped) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %d skipped row(s) to %s.skipped.json\n", len(skipped), outputPath)
			}
			return nil
		},
	}

	bindNormalizeFlags(captureCmd, opts)
	return captureCmd
}

func newAuditFilterCmd() *cobra.Command {
	flagOpts := &auditFilterOptions{}

	filterCmd := &cobra.Command{
		Use:          "filter INPUT_JSON",
		Short:        "Filter a normalized JSON snapshot",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			filters, err := buildAuditFilters(*flagOpts)
			if err != nil {
				return err
			}

			payload, err := audittool.LoadPayload(args[0])
			if err != nil {
				return err
			}

			filtered := audittool.FilterPayload(payload, filters)
			payloadJSON, err := marshalPayload(filtered)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(payloadJSON))
			return err
		},
	}

	bindAuditFilterFlags(filterCmd, flagOpts)
	return filterCmd
}

func newAuditSummaryCmd() *cobra.Command {
	flagOpts := &auditFilterOptions{}

	summaryCmd := &cobra.Command{
		Use:          "summary INPUT_JSON",
		Short:        "Emit a machine-readable summary for normalized transactions",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			filters, err := buildAuditFilters(*flagOpts)
			if err != nil {
				return err
			}

			payload, err := audittool.LoadPayload(args[0])
			if err != nil {
				return err
			}

			filtered := audittool.FilterPayload(payload, filters)
			summary, err := audittool.BuildSummary(filtered)
			if err != nil {
				return err
			}

			summaryJSON, err := audittool.MarshalSummary(summary)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(summaryJSON))
			return err
		},
	}

	bindAuditFilterFlags(summaryCmd, flagOpts)
	return summaryCmd
}

func bindAuditFilterFlags(cmd *cobra.Command, opts *auditFilterOptions) {
	cmd.Flags().StringVar(&opts.txID, "tx-id", "", "Exact match on normalized transaction id")
	cmd.Flags().StringVar(&opts.fromTimestamp, "from-timestamp", "", "Inclusive lower bound on RFC3339 timestamp")
	cmd.Flags().StringVar(&opts.toTimestamp, "to-timestamp", "", "Inclusive upper bound on RFC3339 timestamp")
	cmd.Flags().StringVar(&opts.wallet, "wallet", "", "Exact match on normalized wallet")
	cmd.Flags().StringVar(&opts.asset, "asset", "", "Match sent, received, or fee asset")
	cmd.Flags().StringVar(&opts.rawType, "raw-type", "", "Exact match on normalized raw_type")
}

func buildAuditFilters(flagOpts auditFilterOptions) (audittool.Filters, error) {
	fromTimestamp, err := audittool.ParseTimestamp(flagOpts.fromTimestamp)
	if err != nil {
		return audittool.Filters{}, fmt.Errorf("parse --from-timestamp: %w", err)
	}

	toTimestamp, err := audittool.ParseTimestamp(flagOpts.toTimestamp)
	if err != nil {
		return audittool.Filters{}, fmt.Errorf("parse --to-timestamp: %w", err)
	}

	if fromTimestamp != nil && toTimestamp != nil && fromTimestamp.After(*toTimestamp) {
		return audittool.Filters{}, fmt.Errorf("--from-timestamp must be less than or equal to --to-timestamp")
	}

	return audittool.Filters{
		TxID:          strings.TrimSpace(flagOpts.txID),
		FromTimestamp: fromTimestamp,
		ToTimestamp:   toTimestamp,
		Wallet:        strings.TrimSpace(flagOpts.wallet),
		Asset:         strings.TrimSpace(flagOpts.asset),
		RawType:       strings.TrimSpace(flagOpts.rawType),
	}, nil
}

func captureRerunCommand() string {
	args := append([]string(nil), os.Args...)
	prefix := strings.TrimSpace(os.Getenv("CRYPTOTAX_AUDIT_RERUN_PREFIX"))
	if prefix == "" {
		return audittool.ShellJoin(args)
	}

	if len(args) >= 3 && args[1] == "audit" {
		return strings.TrimSpace(prefix + " " + audittool.ShellJoin(args[2:]))
	}
	return strings.TrimSpace(prefix + " " + audittool.ShellJoin(args[1:]))
}
