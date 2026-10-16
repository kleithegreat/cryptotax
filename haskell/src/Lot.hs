-- | Per-account FIFO lot book (Treas. Reg. §1.1012-1(j)): units leave an
-- account in order of their original acquisition time, and a transfer into
-- the account does not reset that time.
module Lot
  ( Lot (..)
  , Book
  , insert
  , take
  , holding
  , holdingPeriod
  ) where

import           Data.Map.Strict    (Map)
import qualified Data.Map.Strict    as Map
import           Data.Sequence      (Seq)
import qualified Data.Sequence      as Seq
import           Data.Text          (Text)
import           Data.Time          (Day, UTCTime)
import           Data.Time.Calendar (addGregorianYearsRollOver)
import           Prelude            hiding (take)
import           Types              (Term (..))

data Lot = Lot
  { lotQty    :: Rational
  , lotCost   :: Rational -- ^ total USD basis of 'lotQty'
  , lotTime   :: UTCTime  -- ^ acquisition instant, orders FIFO
  , lotDate   :: Day      -- ^ acquisition date, starts the holding period
  , lotOrigin :: Text     -- ^ event that created the lot
  } deriving (Show)

-- | Lots keyed by (account, asset), oldest acquisition first.
type Book = Map (Text, Text) (Seq Lot)

insert :: Text -> Text -> Lot -> Book -> Book
insert account asset lot
  | lotQty lot <= 0 = id
  | otherwise = Map.alter (Just . place . maybe Seq.empty id) (account, asset)
  where
    -- Stable: equal times keep arrival order.
    place lots = case Seq.findIndexR ((<= lotTime lot) . lotTime) lots of
      Nothing -> lot Seq.<| lots
      Just i  -> Seq.insertAt (i + 1) lot lots

-- | Remove @qty@ units FIFO. Returns the slices taken (cost prorated), the
-- quantity that was not there, and the new book.
take :: Text -> Text -> Rational -> Book -> ([Lot], Rational, Book)
take account asset qty book = go qty (Map.findWithDefault Seq.empty (account, asset) book) []
  where
    go need lots acc
      | need <= 0 = done (reverse acc) 0 lots
      | otherwise = case Seq.viewl lots of
          Seq.EmptyL -> done (reverse acc) need Seq.empty
          lot Seq.:< rest
            | lotQty lot <= need -> go (need - lotQty lot) rest (lot : acc)
            | otherwise ->
                let part = lot { lotQty = need, lotCost = lotCost lot * need / lotQty lot }
                    left = lot { lotQty = lotQty lot - need, lotCost = lotCost lot - lotCost part }
                in done (reverse (part : acc)) 0 (left Seq.<| rest)
    done slices missing lots =
      (slices, missing, if Seq.null lots then Map.delete (account, asset) book else Map.insert (account, asset) lots book)

-- | All lots, for holdings reports.
holding :: Book -> [((Text, Text), Lot)]
holding book = [ (k, l) | (k, lots) <- Map.toList book, l <- foldr (:) [] lots ]

-- | Long-term requires holding MORE than one year, counted on local calendar
-- dates. A Feb 29 acquisition rolls to Mar 1, which never overstates
-- long-term treatment.
holdingPeriod :: Day -> Day -> Term
holdingPeriod acquired sold
  | sold > addGregorianYearsRollOver 1 acquired = Long
  | otherwise = Short
