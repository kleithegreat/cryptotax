# EVM Spec

This document defines the intended support boundary for EVM ingestion, normalization, and transfer matching.

## Source of truth

The current EVM pipeline is based on Etherscan source data plus any human-reviewed explorer evidence retained in repository docs.

Agents must not claim a swap, bridge, or own-wallet transfer is fully verified unless repository evidence supports that claim.

## Current support boundary

### Supported or intentionally narrow

- conservative transfer-like handling for straightforward EVM token and native-asset movement
- narrow own-wallet transfer matching where the repository has enough evidence to justify it
- the current confirmed bridge matcher only for the documented same-wallet, cross-chain, same-asset pattern already adopted in the codebase

### Unsupported but surfaced or still approximate

- broad swap reconstruction for arbitrary multi-leg contract interactions
- generalized bridge inference
- semantic collapsing of complex traces into one economic event without source-backed justification

## Rules

- prefer conservative raw-leg preservation over overconfident semantic reconstruction
- bridge and own-wallet heuristics should remain narrow until evidence justifies expansion
- fees and zero-amount artifacts should stay visible when the parser currently emits them and their meaning is still unresolved

## Immediate priority cases

The current highest-priority EVM work is:

- clarifying the supported boundary for transfer matching
- documenting the intentionally narrow bridge exception
- eventually separating clearly supported swap/bridge patterns from current-behavior-only representations
