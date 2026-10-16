{-# LANGUAGE OverloadedStrings #-}

-- | Renders per-tax-year outputs. Amounts are rounded to cents per row
-- (half away from zero) and every printed gain equals printed proceeds minus
-- printed basis, so columns and totals always agree.
module Report
  ( Box (..)
  , box
  , consolidate
  , form8949
  , incomeCSV
  , holdingsCSV
  , reviewCSV
  , exportedCSV
  , summary
  , cents
  , money
  ) where

import           Data.List       (sortOn)
import qualified Data.Map.Strict as Map
import           Data.Ratio      (denominator, numerator)
import           Data.Text       (Text)
import qualified Data.Text       as T
import           Data.Time       (Day, defaultTimeLocale, formatTime)
import           Engine          (Exported (..))
import           Lot             (Lot (..))
import           Types

data Box = A | B | C | D | E | F | G | H | I | J | K | L
  deriving (Eq, Ord, Show)

-- | Form 8949 check box. Digital assets use G–L from tax year 2025 on; before
-- that, crypto with no Form 1099-B goes in C/F.
box :: Integer -> Row -> Box
box year r = case (year >= 2025, rOrigin r, rTerm r) of
  (True, BrokerReported True, Short)  -> G
  (True, BrokerReported False, Short) -> H
  (True, Unreported, Short)           -> I
  (True, BrokerReported True, Long)   -> J
  (True, BrokerReported False, Long)  -> K
  (True, Unreported, Long)            -> L
  (False, BrokerReported True, Short) -> A
  (False, BrokerReported False, Short) -> B
  (False, Unreported, Short)          -> C
  (False, BrokerReported True, Long)  -> D
  (False, BrokerReported False, Long) -> E
  (False, Unreported, Long)           -> F

-- | Merge rows that would print identically apart from amounts: same box
-- source, asset, account, acquisition and sale dates. Sums are exact; the
-- first disposal's event id is kept.
consolidate :: [Row] -> [Row]
consolidate rs = Map.elems (Map.fromListWith merge [ (key r, r) | r <- rs ])
  where
    key r = (rOrigin r, rTerm r, rSold r, rAsset r, rAcquired r, rAccount r)
    merge new old = old { rAmount = rAmount old + rAmount new
                        , rProceeds = rProceeds old + rProceeds new
                        , rBasis = rBasis old + rBasis new
                        , rMerged = rMerged old + rMerged new }

form8949 :: Integer -> [Row] -> Text
form8949 year rs = csv
  ("Box" : "Description" : "Date Acquired" : "Date Sold" : "Proceeds" : "Cost Basis" : "Gain or Loss" : "Term" : "Account" : "Event" : ["Lines Merged"])
  [ [ T.pack (show (box year r)), amount (rAmount r) <> " " <> rAsset r
    , maybe "VARIOUS" date (rAcquired r), date (rSold r)
    , money p, money b, money (p - b)
    , if rTerm r == Long then "Long" else "Short", rAccount r, rEvent r, T.pack (show (rMerged r)) ]
  | r <- sortOn (\r -> (box year r, rSold r, rAsset r)) rs
  , let p = cents (rProceeds r); b = cents (rBasis r) ]

incomeCSV :: [IncomeRow] -> Text
incomeCSV is = csv ["Date", "Type", "Asset", "Amount", "USD Value", "Account", "Event"]
  [ [date (iDate i), iType i, iAsset i, amount (iAmount i), money (cents (iUSD i)), iAccount i, iEvent i] | i <- is ]

holdingsCSV :: (Text -> Text) -> [((Text, Text), Lot)] -> Text
holdingsCSV sym hs = csv ["Account", "Asset", "Amount", "Cost Basis", "Date Acquired", "From Event"]
  [ [acct, sym asset, amount (lotQty l), money (cents (lotCost l)), date (lotDate l), lotOrigin l] | ((acct, asset), l) <- hs ]

reviewCSV :: [Issue] -> Text
reviewCSV is = csv ["Severity", "Event", "Message"]
  [ [if isBlocking i then "BLOCKING" else "info", isEvent i, isMessage i] | i <- sortOn (not . isBlocking) is ]

exportedCSV :: (Text -> Text) -> [Exported] -> Text
exportedCSV sym es = csv ["Date Sent", "Account", "Asset", "Amount", "Cost Basis", "Date Acquired", "Event"]
  [ [date (exDate e), exAccount e, sym (exAsset e), amount (lotQty (exLot e)), money (cents (lotCost (exLot e))), date (lotDate (exLot e)), exEvent e] | e <- es ]

-- | Totals per Form 8949 box mapped to Schedule D lines, plus income.
summary :: Integer -> Int -> [Row] -> [IncomeRow] -> Text
summary year blocking rs is = T.unlines $
  [ "Tax year " <> T.pack (show year)
  , if blocking == 0 then "Status: FINAL (no blocking review items)"
    else "Status: PROVISIONAL — " <> T.pack (show blocking) <> " blocking review item(s) in review.csv; do not file from this"
  , ""
  , "Form 8949 / Schedule D        Proceeds        Basis    Gain/Loss  Rows"
  ] ++
  [ T.justifyLeft 26 ' ' (T.pack (show bx) <> " → Schedule D line " <> line bx)
      <> col (sum ps) <> col (sum bs) <> col (sum ps - sum bs) <> "  " <> T.pack (show (length ps))
  | (bx, prs) <- Map.toList byBox, let ps = map fst prs; bs = map snd prs ] ++
  [ ""
  , "Net short-term: " <> money (net Short)
  , "Net long-term:  " <> money (net Long)
  , ""
  , "Ordinary income (Schedule 1, " <> (if year >= 2024 then "line 8v, digital assets" else "line 8z, other income") <> "):"
  ] ++
  [ "  " <> T.justifyLeft 14 ' ' t <> money v | (t, v) <- Map.toList incomeByType ] ++
  [ "  total         " <> money (sum (Map.elems incomeByType)) ]
  where
    byBox = Map.fromListWith (flip (++)) [ (box year r, [(cents (rProceeds r), cents (rBasis r))]) | r <- rs ]
    net t = sum [ cents (rProceeds r) - cents (rBasis r) | r <- rs, rTerm r == t ]
    incomeByType = Map.fromListWith (+) [ (iType i, cents (iUSD i)) | i <- is ]
    col v = T.justifyRight 13 ' ' (money v)
    line bx = case bx of
      A -> "1b"; G -> "1b"; B -> "2"; H -> "2"; C -> "3"; I -> "3"
      D -> "8b"; J -> "8b"; E -> "9"; K -> "9"; F -> "10"; L -> "10"

-- | Round to whole cents, half away from zero.
cents :: Rational -> Integer
cents r =
  let s = r * 100
      n = abs (numerator s)
      d = denominator s
      q = (2 * n + d) `div` (2 * d)
  in if s < 0 then negate q else q

money :: Integer -> Text
money c =
  let (d, m) = abs c `divMod` 100
      s = T.pack (show d) <> "." <> T.justifyRight 2 '0' (T.pack (show m))
  in if c < 0 then "-" <> s else s

-- | Quantity with up to 18 decimals, trailing zeros trimmed.
amount :: Rational -> Text
amount r =
  let scaled = round (abs r * 10 ^ (18 :: Int)) :: Integer
      (w, f) = scaled `divMod` (10 ^ (18 :: Int))
      frac = T.dropWhileEnd (== '0') (T.justifyRight 18 '0' (T.pack (show f)))
  in (if r < 0 then "-" else "") <> T.pack (show w) <> (if T.null frac then "" else "." <> frac)

date :: Day -> Text
date = T.pack . formatTime defaultTimeLocale "%m/%d/%Y"

csv :: [Text] -> [[Text]] -> Text
csv header body = T.unlines (map (T.intercalate "," . map field) (header : body))
  where
    field f
      | T.any (`elem` [',', '"', '\n', '\r']) g = "\"" <> T.replace "\"" "\"\"" g <> "\""
      | otherwise = g
      where
        -- Spreadsheet formula guard; '-' stays so negative amounts read as numbers.
        g = case T.uncons f of
          Just (c, _) | c `elem` ['=', '+', '@'] -> "'" <> f
          _ -> f
