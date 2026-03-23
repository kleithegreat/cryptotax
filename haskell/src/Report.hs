{-# LANGUAGE OverloadedStrings #-}

module Report
  ( render8949CSV
  ) where

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
  T.intercalate ","
    [ renderDescription gl
    , formatDate (glAcquired gl)
    , formatDate (glDisposed gl)
    , renderUSD (glProceeds gl)
    , renderUSD (glCostBasis gl)
    , renderUSD (glGain gl)
    , case glPeriod gl of
        ShortTerm -> "Short"
        LongTerm  -> "Long"
    ]

renderDescription :: GainLoss -> Text
renderDescription gl = renderAmount (glAmount gl) <> " " <> unAsset (glAsset gl)

formatDate :: UTCTime -> Text
formatDate = T.pack . formatTime defaultTimeLocale "%m/%d/%Y"

-- | Render a USD value with 2 decimal places. Negative in parentheses per IRS.
renderUSD :: USD -> Text
renderUSD (USD r) =
  let cents = round (r * 100) :: Integer
      (dollars, remainCents) = abs cents `divMod` 100
      formatted = T.pack (show dollars) <> "." <> padZero (T.pack (show remainCents))
  in if cents < 0
     then "(" <> formatted <> ")"
     else formatted

-- | Render a token amount with up to 8 decimal places.
renderAmount :: TokenAmount -> Text
renderAmount (TokenAmount r) =
  let scaled = round (r * 100000000) :: Integer
      (whole, frac) = abs scaled `divMod` 100000000
      sign = if scaled < 0 then "-" else ""
      fracStr = T.dropWhileEnd (== '0') (padN 8 (T.pack (show frac)))
  in sign <> T.pack (show whole) <> if T.null fracStr then "" else "." <> fracStr

padZero :: Text -> Text
padZero t = if T.length t < 2 then "0" <> t else t

padN :: Int -> Text -> Text
padN n t = T.replicate (max 0 (n - T.length t)) "0" <> t
