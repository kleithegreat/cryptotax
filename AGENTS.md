# AGENTS.md

This repository is a crypto tax report generator with:
- Go plumbing in `./go`
- Haskell financial core in `./haskell`
- Nix flake workflow at the repo root

## Ground rules

- Do not reintroduce a Makefile.
- Prefer flake-native workflows: `nix build`, `nix run`, `nix flake check`, `nix develop`.
- Do not edit Repomix-packed snapshots as if they were source files.
- Do not leak secrets in help text, logs, defaults, test fixtures, or docs.
- Do not silently invent tax behavior for unsupported cases. Prefer:
  - explicit TODOs
  - clear errors
  - explicit “approximate” or “unmodeled” handling
- Keep changes small, coherent, and production-quality.

## Required validation after code changes

After changing Go, Haskell, flake, or transaction-classification logic, run:

```bash
nix build .#cli
nix build .#core
nix flake check
```

If Go-only unit tests are relevant, run them from the nested module correctly, for example:

```bash
nix develop -c bash -lc 'cd go && go test ./...'
```

If you change only docs, say so explicitly and skip build steps only if they are genuinely unnecessary.

## Repository expectations

### Nix

* Use flake outputs, not ad hoc shell wrappers, for supported workflows.
* For Go builds:

  * `src = ./.;`
  * `modRoot = "./go";`
  * `subPackages` should point at the desired command package(s)
* If `vendorHash` changes, update it deliberately and report the final value.

### Go

* Reuse existing types where possible.
* Prefer adding typed helpers or subcommands over shell-script plumbing.
* When changing normalization or fetcher behavior, add or update Go tests.
* Avoid brittle stringly-typed logic when structured logic is possible.

### Haskell

* Preserve exact arithmetic semantics.
* Do not introduce floating-point behavior into tax math.
* If changing lot or gain/loss behavior, keep or improve property-test coverage.

## Tax-accuracy policy

This codebase is aiming for high tax accuracy, not just passing builds.

When changing transaction semantics:

* Do not collapse complex multi-leg activity into simplistic classifications unless that is already an intentional limitation.
* If behavior is approximate, say so in code comments and in your summary.
* Unsupported or partially modeled cases should be surfaced, not hidden.

When dealing with real transactions:
- Codex may summarize and prepare evidence.
- Codex must not claim a transaction is verified unless the evidence is present in repo docs or explicitly supplied by the user.
- Leave placeholders for human confirmation when needed.

High-risk areas:

* EVM swaps and multi-leg contract interactions
* Bridges / own-wallet cross-chain transfers
* Solana multi-transfer transactions and fee attribution
* Hyperliquid perpetuals and funding
* Asset symbol resolution and USD valuation gaps

## Audit tooling policy

* Do not add new shell scripts for core audit workflows unless explicitly asked.
* Prefer Go-based audit tooling integrated into the CLI or a dedicated Go command.
* Audit tools should support:

  * capture normalized output
  * filter by tx id / wallet / asset / raw_type / timestamp range
  * reconciliation summaries
* Audit tools should emit stable, machine-readable output.

When triaging production data:
- prioritize `zero_usd_value_rows`
- prioritize `suspicious_asset_rows`
- prioritize unusual `tx_type` or `source` buckets
- propose candidate verified cases, but do not fabricate conclusions

## Test expectations

When fixing a bug:

* Add or update a focused regression test.

When changing classification behavior:

* Add or update:

  * unit tests for the affected component, and/or
  * an end-to-end fixture / golden test if the change crosses the Go→Haskell boundary

Preferred additions:

* small fixture inputs
* exact expected outputs
* no external network calls in tests

## Docs expectations

If user-facing workflow changes:

* update `README.md`
* update or add docs under `docs/`

Do not leave the docs describing deleted tooling or obsolete commands.

## Final response format

At the end of your work, report:

1. what changed
2. why it changed
3. how you validated it
4. what was observed in the audit output
5. which cases still require human verification
6. any remaining TODOs / limitations
7. exact commands the user should run next