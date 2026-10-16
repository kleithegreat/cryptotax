// Command cryptotax builds US tax reports from on-chain and exchange history.
//
//	cryptotax report  fetch, classify, run the core, write reports/
//	cryptotax events  print the classified IR (what the core would receive)
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kevin/cryptotax/config"
	"github.com/kevin/cryptotax/evm"
	"github.com/kevin/cryptotax/hyperliquid"
	"github.com/kevin/cryptotax/ledger"
	"github.com/kevin/cryptotax/netcache"
	"github.com/kevin/cryptotax/pricing"
	"github.com/kevin/cryptotax/reconcile"
	"github.com/kevin/cryptotax/robinhood"
	"github.com/kevin/cryptotax/solana"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", "cryptotax.json", "configuration file")
	cacheDir := fs.String("cache", ".cache", "source response cache (the evidence trail)")
	offline := fs.Bool("offline", false, "use only cached source responses")
	outDir := fs.String("out", "reports", "report output directory")
	core := fs.String("core", "cryptotax-core", "path to the Haskell core")
	_ = fs.Parse(args)

	var err error
	switch cmd {
	case "events":
		var p ledger.Payload
		if p, err = build(*cfgPath, *cacheDir, *offline); err == nil {
			err = json.NewEncoder(os.Stdout).Encode(p)
		}
	case "report":
		err = report(*cfgPath, *cacheDir, *offline, *outDir, *core)
	default:
		usage()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cryptotax report|events [--config cryptotax.json] [--cache .cache] [--offline] [--out reports] [--core cryptotax-core]")
	os.Exit(2)
}

func report(cfgPath, cacheDir string, offline bool, outDir, core string) error {
	p, err := build(cfgPath, cacheDir, offline)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	ir, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "events.json"), ir, 0o644); err != nil {
		return err
	}
	c := exec.Command(core, "--out", outDir)
	c.Stdin = bytes.NewReader(ir)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func build(cfgPath, cacheDir string, offline bool) (ledger.Payload, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return ledger.Payload{}, err
	}
	// Relative paths in the config are relative to the config file.
	base := filepath.Dir(cfgPath)
	net := netcache.New(cacheDir, offline)

	var ms []ledger.Movement
	var direct []ledger.Event
	var issues []ledger.Issue

	if len(cfg.Wallets.EVM) > 0 {
		key, err := secret("ETHERSCAN_API_KEY", offline)
		if err != nil {
			return ledger.Payload{}, err
		}
		f := &evm.Fetcher{Net: net, APIKey: key}
		for _, w := range cfg.Wallets.EVM {
			for _, chain := range cfg.EVMChains {
				log("%s %s", chain, w)
				got, err := f.Movements(chain, w)
				if err != nil {
					return ledger.Payload{}, err
				}
				ms = append(ms, got...)
			}
		}
	}
	if len(cfg.Wallets.Solana) > 0 {
		key, err := secret("HELIUS_API_KEY", offline)
		if err != nil {
			return ledger.Payload{}, err
		}
		f := &solana.Fetcher{Net: net, APIKey: key}
		for _, w := range cfg.Wallets.Solana {
			log("solana %s", w)
			got, err := f.Movements(w)
			if err != nil {
				return ledger.Payload{}, err
			}
			ms = append(ms, got...)
		}
	}
	hl := &hyperliquid.Fetcher{Net: net}
	for _, w := range cfg.Wallets.Hyperliquid {
		log("hyperliquid %s", w)
		r, err := hl.Read(w)
		if err != nil {
			return ledger.Payload{}, err
		}
		ms = append(ms, r.Movements...)
		direct = append(direct, r.Events...)
		issues = append(issues, r.Issues...)
	}
	for _, path := range cfg.Robinhood1099 {
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		log("robinhood %s", path)
		evs, err := robinhood.Read(path, cfg.Location)
		if err != nil {
			return ledger.Payload{}, err
		}
		direct = append(direct, evs...)
	}

	log("reconciling balances")
	rec, err := reconcile.Check(cfg, net, ms, direct)
	if err != nil {
		return ledger.Payload{}, err
	}
	issues = append(issues, rec...)

	log("pricing and classifying %d movements", len(ms))
	pr := pricing.New(net)
	p := ledger.Build(cfg, ms, direct, issues, pr)
	return p, nil
}

func secret(name string, offline bool) (string, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" && !offline {
		return "", fmt.Errorf("%s is not set", name)
	}
	return v, nil
}

func log(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "• "+format+"\n", args...)
}
