# Hyperliquid Spec

This document defines the intended support boundary for Hyperliquid ingestion, normalization, and downstream accounting.

## Source of truth

The current Hyperliquid pipeline is based on Hyperliquid API data plus any human-reviewed evidence retained in repository docs.

Agents must preserve source-backed details and avoid presenting perp activity as fully solved spot semantics when that is not actually true.

## Current support boundary

### Supported or intentionally modeled

- funding rows represented explicitly as `funding_payment`
- positive funding preserved as received USDC and available to downstream income handling
- negative funding preserved as sent USDC rather than dropped

### Unsupported but surfaced or still approximate

- perpetual opening and closing activity that is currently approximated too much like spot
- multi-row fill groupings that may represent one economic perp event
- final tax-output semantics for negative funding expense handling

## Rules

- do not silently erase funding outflows
- do not overclaim semantic certainty for perp fills
- preserve enough information for later accounting improvements and human review

## Immediate priority cases

The current highest-priority Hyperliquid work is:

- evidence retention and linkage for funding rows
- explicit support-boundary documentation for perp open/close cases
- eventual separation of true spot-like inventory changes from perp position changes
