# Repository Quirks

This document captures repository-wide gotchas that are easy for agents to miss from code alone.

## 1. Not every fixture means the same thing

The repository contains both:

- exact end-to-end golden fixtures
- current-behavior real-wallet regression fixtures

Do not treat the second category as proof of tax correctness. They often freeze the current observed normalization only.

## 2. The strongest rule in this repo is honesty about unsupported behavior

When in doubt, preserve ambiguity and surface it. Do not compress an uncertain case into a neat but misleading answer.

## 3. Validation should be agent-run, not human-delegated

The repository prefers agents to perform validation and report the actual results. Avoid ending work with a handoff that is mostly a list of shell commands for the human.

## 4. Real-wallet review docs are evidence logs, not just issue lists

`docs/known-transactions.md` is not merely a backlog. It currently functions as a human-reviewed evidence log. Changes there should preserve that role until a more structured replacement exists.

## 5. Source-specific weirdness should move into domain quirks docs over time

As the docs hierarchy matures, repository-wide quirks should stay minimal. Source-specific gotchas should live in their own domain `QUIRKS.md` files. Current domain quirks coverage: `docs/evm/QUIRKS.md` (Etherscan spam symbols, zero-amount artifacts), `docs/solana/QUIRKS.md` (Helius mint-as-asset, float64 rendering, fee attachment, endpoint fallback), `docs/hyperliquid/QUIRKS.md` (all-zero funding hash, older fixtures that predate the `market`/`event_group_id` upgrade, `@N` resolution, partial fill sharing).
