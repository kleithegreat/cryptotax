{-# LANGUAGE OverloadedStrings #-}

module Report
  ( render8949CSV
  , renderIncomeCSV
  , renderFundingExpenseCSV
  , renderPerpPnlCSV
  , renderTransfersCSV
  , renderUSD
  ) where

import           Data.Maybe   (fromMaybe)
import           Data.Ratio   (numerator, denominator)
import           Data.Text    (Text)
import qualified Data.Text    as T
import           Data.Time    (UTCTime, formatTime, defaultTimeLocale)
import           Types

-- | Render GainLoss events as a Form 8949 compatible CSV.
render8949CSV :: [GainLoss] -> Text
render8949CSV gains =
  let header = "Description,Date Acquired,Date Sold,Proceeds,Cost Basis,Gain or Loss,Term"
      rows   = map renderRow gains
  in T.unlines (header : rows)

renderRow :: GainLoss -> Text
renderRow gl =
  -- The gain column is rendered from the SUBTRACTION OF THE ROUNDED CENTS of
  -- proceeds and basis, so the printed columns always satisfy
  -- Gain = Proceeds - Cost Basis. Display-level only; internal Rationals are
  -- untouched.
  let proceedsCents = usdCents (glProceeds gl)
      basisCents    = usdCents (glCostBasis gl)
  in csvRow
    [ renderDescription gl
    , formatDate (glAcquired gl)
    , formatDate (glDisposed gl)
    , renderCents proceedsCents
    , renderCents basisCents
    , renderCents (proceedsCents - basisCents)
    , case glPeriod gl of
        ShortTerm -> "Short"
        LongTerm  -> "Long"
    ]

renderDescription :: GainLoss -> Text
renderDescription gl = renderAmount (glAmount gl) <> " " <> unAsset (glAsset gl)

formatDate :: UTCTime -> Text
formatDate = T.pack . formatTime defaultTimeLocale "%m/%d/%Y"

-- | Round a USD value to whole cents, half away from zero (NOT banker's
-- rounding, which would make printed columns disagree on .5 boundaries).
usdCents :: USD -> Integer
usdCents (USD r) =
  let scaled = r * 100
      n = abs (numerator scaled)
      d = denominator scaled
      q = (2 * n + d) `div` (2 * d)
  in if scaled < 0 then negate q else q

-- | Render whole cents with 2 decimal places. Negative in parentheses per IRS.
renderCents :: Integer -> Text
renderCents cents =
  let (dollars, remainCents) = abs cents `divMod` 100
      formatted = T.pack (show dollars) <> "." <> padZero (T.pack (show remainCents))
  in if cents < 0
     then "(" <> formatted <> ")"
     else formatted

-- | Render a USD value with 2 decimal places, rounding half away from zero.
renderUSD :: USD -> Text
renderUSD = renderCents . usdCents

-- | Render a token amount with up to 18 decimal places (full wei precision)
-- so sub-1e-8 disposals do not display as zero.
renderAmount :: TokenAmount -> Text
renderAmount (TokenAmount r) =
  let scaled = round (r * 10 ^ (18 :: Int)) :: Integer
      (whole, frac) = abs scaled `divMod` (10 ^ (18 :: Int))
      sign = if scaled < 0 then "-" else ""
      fracStr = T.dropWhileEnd (== '0') (padN 18 (T.pack (show frac)))
  in sign <> T.pack (show whole) <> if T.null fracStr then "" else "." <> fracStr

padZero :: Text -> Text
padZero t = if T.length t < 2 then "0" <> t else t

padN :: Int -> Text -> Text
padN n t = T.replicate (max 0 (n - T.length t)) "0" <> t

-- ---------------------------------------------------------------------------
-- Supplemental reports — separate from 8949
-- ---------------------------------------------------------------------------

-- | Render ordinary income as a structured CSV. The Market column carries the
-- source market context when preserved (empty when absent); committed
-- Hyperliquid funding rows share an all-zero hash, so Market is the only
-- useful discriminator there.
renderIncomeCSV :: [IncomeEntry] -> Text
renderIncomeCSV entries =
  let header = "Date,Tx ID,Asset,Amount,USD Value,Market"
      rows   = map renderIncomeRow entries
  in T.unlines (header : rows)

renderIncomeRow :: IncomeEntry -> Text
renderIncomeRow ii =
  csvRow
    [ formatDate (iiTimestamp ii)
    , iiTxId ii
    , unAsset (iiAsset ii)
    , renderAmount (iiAmount ii)
    , renderUSD (iiUSDValue ii)
    , fromMaybe "" (iiMarket ii)
    ]

-- | Render funding expenses as a structured CSV. See 'renderIncomeCSV' for
-- the Market column rationale.
renderFundingExpenseCSV :: [FundingExpense] -> Text
renderFundingExpenseCSV expenses =
  let header = "Date,Tx ID,Asset,Amount,USD Value,Market"
      rows   = map renderFundingRow expenses
  in T.unlines (header : rows)

renderFundingRow :: FundingExpense -> Text
renderFundingRow fe =
  csvRow
    [ formatDate (feTimestamp fe)
    , feTxId fe
    , unAsset (feAsset fe)
    , renderAmount (feAmount fe)
    , renderUSD (feUSDValue fe)
    , fromMaybe "" (feMarket fe)
    ]

-- | Render transfer activity as a structured CSV. Transfers are not taxable
-- events; this output keeps them visible instead of silently dropping them.
renderTransfersCSV :: [TransferEntry] -> Text
renderTransfersCSV entries =
  let header = "Date,Tx ID,Direction,Asset,Amount,USD Value,Wallet,Counterparty"
      rows   = map renderTransferRow entries
  in T.unlines (header : rows)

renderTransferRow :: TransferEntry -> Text
renderTransferRow te =
  csvRow
    [ formatDate (teTimestamp te)
    , teTxId te
    , case teDirection te of
        DirIn  -> "In"
        DirOut -> "Out"
    , unAsset (teAsset te)
    , renderAmount (teAmount te)
    , renderUSD (teUSDValue te)
    , teWallet te
    , fromMaybe "" (teCounterparty te)
    ]

-- | Render perp realized PnL as a structured CSV.
-- This is intentionally separate from 8949 because the cost-basis/proceeds
-- representation for derivative PnL on Form 8949 is unresolved.
renderPerpPnlCSV :: [PerpPnlEntry] -> Text
renderPerpPnlCSV entries =
  let header = "Date,Tx ID,Asset,Amount,Direction,Realized PnL"
      rows   = map renderPerpRow entries
  in T.unlines (header : rows)

renderPerpRow :: PerpPnlEntry -> Text
renderPerpRow pp =
  csvRow
    [ formatDate (ppTimestamp pp)
    , ppTxId pp
    , unAsset (ppAsset pp)
    , renderAmount (ppAmount pp)
    , ppDirection pp
    , renderUSD (ppClosedPnl pp)
    ]

csvRow :: [Text] -> Text
csvRow = T.intercalate "," . map csvField

csvField :: Text -> Text
csvField field
  | T.any needsQuoting guarded = "\"" <> T.replace "\"" "\"\"" guarded <> "\""
  | otherwise = guarded
  where
    -- Spreadsheet formula-injection guard: '=', '+', '@' can start a formula
    -- in Excel/Sheets, so prefix a literal quote. '-' is deliberately NOT
    -- guarded so negative numbers stay clean.
    guarded = case T.uncons field of
      Just (c, _) | c == '=' || c == '+' || c == '@' -> "'" <> field
      _                                              -> field
    needsQuoting c = c == ',' || c == '"' || c == '\r' || c == '\n'
