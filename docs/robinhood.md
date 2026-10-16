# Robinhood

## Intent

- The source is the Form 1099-DA section of Robinhood's consolidated 1099
  CSV (tax year 2025 on). Each row is a broker sale in the tracked
  `robinhood` account.
- Robinhood is only partly observed: purchases made there are not in the
  data. Deposits reach the account through `account` counterparty labels on
  the deposit addresses. Withdrawals come from Robinhood hot wallets labeled
  `account` with `"basis": "fmv"`, so units never seen arriving (bought on
  Robinhood) take their value at withdrawal as basis. That is an
  approximation of the purchase price, chosen explicitly in the config.

## Quirks

- On transferred-in rows (8949 code `X`, "DA NUM UNITS TRANS IN" filled) a
  COST BASIS of `0` means *unknown*, not zero; those units come from tracked
  lots.
- Rows marked aggregated have no acquisition date (printed as VARIOUS).
  Aggregated stablecoin rows have no sale date at all and are dated Dec 31
  of the tax year.
- Robinhood withdrawals are co-signed by a fixed Robinhood fee-payer account;
  that is how its hot wallets were identified.
- Crypto sales before 2025 were reported on Form 1099-B; that format is not
  read.
