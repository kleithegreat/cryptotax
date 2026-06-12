{-# LANGUAGE OverloadedStrings #-}

module Lot
  ( LotQueue
  , empty
  , acquire
  , dispose
  , holdingPeriod
  , totalCostBasis
  , totalUnits
  ) where

import           Data.Map.Strict    (Map)
import qualified Data.Map.Strict    as Map
import           Data.Ratio         (numerator, denominator)
import           Data.Text          (Text)
import qualified Data.Text          as T
import           Data.Time          (UTCTime, utctDay)
import           Data.Time.Calendar (addGregorianYearsRollOver)
import           Types

-- | Per-asset FIFO queue. Each asset has a list of lots, oldest first.
type LotQueue = Map AssetSymbol [TaxLot]

empty :: LotQueue
empty = Map.empty

-- | Record an acquisition. Appends a new lot to the BACK of the queue (FIFO).
acquire :: AssetSymbol -> UTCTime -> TokenAmount -> USD -> LotQueue -> LotQueue
acquire asset timestamp amount costBasisUSD queue =
  if amount == 0
    then queue
    else
      let lot = TaxLot
            { lotAsset       = asset
            , lotAcquired    = timestamp
            , lotRemaining   = amount
            , lotCostPerUnit = unUSD costBasisUSD / unTokens amount
            }
      in Map.insertWith (\_ old -> old ++ [lot]) asset [lot] queue

-- | Record a disposal. Pops lots from the FRONT (FIFO) and returns gain/loss
-- records plus the updated queue. Errors if insufficient lots exist or the
-- disposal amount is negative (a contract violation, never a zero-gain no-op).
dispose :: Disposal -> LotQueue -> Either Text ([GainLoss], LotQueue)
dispose disp queue
  | dispAmount disp < 0 =
      Left $ "Negative disposal amount for " <> unAsset (dispAsset disp)
           <> ": " <> showR (unTokens (dispAmount disp))
  | otherwise =
      let asset = dispAsset disp
          lots  = Map.findWithDefault [] asset queue
      in case consumeLots disp (dispAmount disp) lots [] of
           Left err             -> Left err
           Right (gains, lots') -> Right (gains, updateLots asset lots' queue)

-- | Walk lots FIFO, consuming units until the disposal is satisfied.
-- The Disposal is never mutated; @toConsume@ tracks remaining units.
-- This ensures the proportional proceeds allocation uses the original
-- total disposal amount as the denominator (fixing the multi-lot bug).
consumeLots :: Disposal -> TokenAmount -> [TaxLot] -> [GainLoss]
            -> Either Text ([GainLoss], [TaxLot])
consumeLots _disp toConsume [] acc
  | toConsume <= 0 = Right (reverse acc, [])
  | otherwise      = Left $ "Insufficient lots for " <> unAsset (dispAsset _disp)
                          <> ": need " <> showR (unTokens toConsume) <> " more units"
consumeLots disp toConsume (lot : lots) acc
  | toConsume <= 0 = Right (reverse acc, lot : lots)
  | lotRemaining lot <= 0 = consumeLots disp toConsume lots acc
  | lotRemaining lot <= toConsume =
      -- Consume the entire lot
      let consumed  = lotRemaining lot
          basis     = USD (unTokens consumed * lotCostPerUnit lot)
          proportion = unTokens consumed / unTokens (dispAmount disp)
          proceeds  = USD (proportion * unUSD netProceeds)
          gain      = mkGain disp lot consumed basis proceeds
      in consumeLots disp (toConsume - consumed) lots (gain : acc)
  | otherwise =
      -- Partially consume this lot
      let basis     = USD (unTokens toConsume * lotCostPerUnit lot)
          proportion = unTokens toConsume / unTokens (dispAmount disp)
          proceeds  = USD (proportion * unUSD netProceeds)
          gain      = mkGain disp lot toConsume basis proceeds
          lot'      = lot { lotRemaining = lotRemaining lot - toConsume }
      in Right (reverse (gain : acc), lot' : lots)
  where
    netProceeds = dispProceeds disp - dispFee disp

mkGain :: Disposal -> TaxLot -> TokenAmount -> USD -> USD -> GainLoss
mkGain disp lot amount basis proceeds = GainLoss
  { glAsset     = dispAsset disp
  , glAcquired  = lotAcquired lot
  , glDisposed  = dispTime disp
  , glAmount    = amount
  , glCostBasis = basis
  , glProceeds  = proceeds
  , glGain      = proceeds - basis
  , glPeriod    = holdingPeriod (lotAcquired lot) (dispTime disp)
  }

-- | IRS holding-period rule: long-term requires holding MORE than one year.
-- A sale on the one-year anniversary calendar date is still short-term, so the
-- comparison is calendar-based on UTC trade dates rather than elapsed days.
-- Feb 29 acquisitions roll over to Mar 1 (addGregorianYearsRollOver), the
-- conservative choice that never overstates long-term treatment.
holdingPeriod :: UTCTime -> UTCTime -> HoldingPeriod
holdingPeriod acquired disposed =
  if utctDay disposed > addGregorianYearsRollOver 1 (utctDay acquired)
    then LongTerm
    else ShortTerm

-- | Total cost basis across all lots in the queue (for invariant checking).
totalCostBasis :: LotQueue -> USD
totalCostBasis = Map.foldl' (\acc lots -> acc + sum (map lotBasis lots)) 0
  where lotBasis l = USD (unTokens (lotRemaining l) * lotCostPerUnit l)

-- | Total units of an asset in the queue.
totalUnits :: AssetSymbol -> LotQueue -> TokenAmount
totalUnits asset queue =
  sum $ map lotRemaining $ Map.findWithDefault [] asset queue

updateLots :: AssetSymbol -> [TaxLot] -> LotQueue -> LotQueue
updateLots asset lots queue
  | null lots  = Map.delete asset queue
  | otherwise  = Map.insert asset lots queue

-- | Render a Rational for error messages. Fractional digits are computed to
-- 18 places (full wei precision) so sub-1e-6 shortfalls are not shown as zero;
-- when the trimmed fractional part is empty, only the integer part is shown.
showR :: Rational -> Text
showR r =
  let sign = if r < 0 then "-" else ""
      r' = abs r
      n = numerator r'
      d = denominator r'
      (q, rem') = n `divMod` d
      fracDigits = trimTrailingZeroes (pad18 (T.pack (show (rem' * 10 ^ (18 :: Int) `div` d))))
  in if T.null fracDigits
     then sign <> T.pack (show q)
     else sign <> T.pack (show q) <> "." <> fracDigits

pad18 :: Text -> Text
pad18 t = T.replicate (max 0 (18 - T.length t)) "0" <> t

trimTrailingZeroes :: Text -> Text
trimTrailingZeroes = T.dropWhileEnd (== '0')
