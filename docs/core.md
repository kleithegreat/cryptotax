# Core — tax accounting

## Intent

The Haskell core turns classified events into tax-year reports. It is the
only place tax rules live; the Go side never decides a tax outcome.

- **Exact arithmetic.** Every amount is a `Rational`. Rounding happens only
  when a row is printed: per row, to cents, half away from zero, and the
  printed gain is printed proceeds minus printed basis, so columns and totals
  always agree.
- **Lots are per account, FIFO by original acquisition time, for every year.**
  Treas. Reg. §1.1012-1(j) requires wallet-by-wallet identification from
  2025-01-01, with FIFO as the default absent a contemporaneous specific
  identification, and disregards the date units were transferred into the
  wallet. Applying the same rule to 2023–2024 means there was never a
  universal pool, so no Rev. Proc. 2024-28 transition allocation is needed.
  Specific identification is not offered: it requires records made at the
  time of each sale, which this tool cannot produce retroactively.
- **One value per exchange** (§1001): a cash (stablecoin) side is exact and
  wins; otherwise the fair market value received (the amount realized);
  otherwise the value given up. If both sides are priced and differ by more
  than 10%, part of the exchange happened elsewhere and the event blocks until
  the taxpayer pairs or confirms it.
- **Transaction costs** (§1.1001-7(b)): reduce the amount realized when
  property is disposed of, and increase basis when property is bought with
  cash; in a crypto-for-crypto exchange they go entirely to the disposed
  side. A fee paid in crypto is itself a disposal at fair market value.
- **Fees on non-exchanges** (transfers, approvals, failed transactions) are
  disposals of the fee coin; their value is a nondeductible personal cost
  (§67(g)).
- **Own-wallet transfers** are not taxable. Lots move with their original
  dates. Units lost in transit (bridge or relayer fees) are spent at fair
  market value.
- **USD stablecoins are cash at $1**: no lots, no gain. This is an
  approximation that ignores depegs (e.g. USDC in March 2023); the stablecoin
  list is in docs/assets.md. Broker-reported stablecoin sales are still
  printed (at par) so the 8949 matches the broker's Form 1099-DA.
- **Income** (airdrops, rewards, value received from third parties) is
  ordinary income at fair market value on receipt and becomes basis. It goes
  on Schedule 1 line 8v (line 8z before 2024). Unsolicited worthless tokens
  and dust are not income; they get a $0 basis.
- **Perpetual futures are capital assets, not §1256 contracts** (Hyperliquid
  is not a qualified board or exchange). Each close realizes the exchange's
  gross PnL minus the close fee and the open fees carried by the closed
  slices. The holding period comes from FIFO position slices. Rows are
  printed net (proceeds = gain or 0, basis = loss or 0).
- **Funding is part of the derivative's capital result**, recognized when
  paid or received and netted per market per local day. Treating funding as
  ordinary income with nondeductible payments (a notional-principal-contract
  reading) is the main alternative; this choice is applied consistently and
  worth a Form 8275 disclosure for material amounts.
- **Form 1099-DA rows**: when the broker states its own basis and term, the
  row is reported as printed. Otherwise the units came from elsewhere and are
  taken from the broker account's lots. The box is G/H/J/K depending on
  whether basis was reported to the IRS.
- **Form 8949 boxes**: from tax year 2025, digital assets not on a 1099-DA go
  in box I (short) or L (long). Earlier years use C and F. Rows that would
  print identically apart from amounts (same box, asset, account, acquisition
  and sale date) are merged exactly.
- **Dates are local calendar dates** in the configured timezone. Long-term
  means held more than one year by calendar date; a Feb 29 acquisition rolls
  to Mar 1, which never overstates long-term treatment.
- **Missing history never hides.** Units disposed of but never seen arriving
  get a provisional $0 basis dated the sale day and a blocking review item.
  Any blocking item makes every report PROVISIONAL (exit code 3).

## Quirks

- FIFO order is acquisition time, not arrival order. A move inserts old lots
  ahead of newer ones already in the destination; appending them would
  silently break the per-wallet rule.
- A year's perp total does not equal the exchange's realized PnL minus fees
  while a position is open: open fees sit in position slices until the close.
- Funding rows print an amount of 0: they are cash flows, not units.
