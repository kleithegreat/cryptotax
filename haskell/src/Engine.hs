{-# LANGUAGE MultiWayIf        #-}
{-# LANGUAGE OverloadedStrings #-}

-- | Applies IR events to the lot book and produces Form 8949 rows, income
-- and review issues. Tax treatments are documented in docs/core.md.
module Engine
  ( Result (..)
  , Exported (..)
  , run
  ) where

import           Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import           Data.Maybe      (fromMaybe, isNothing, mapMaybe)
import           Data.Sequence   (Seq)
import qualified Data.Sequence   as Seq
import           Data.Text       (Text)
import qualified Data.Text       as T
import           Data.Time       (Day, UTCTime (..), toGregorian)
import           Lot             (Book, Lot (..), holdingPeriod)
import qualified Lot
import           Types

-- | Lots sent to the taxpayer's own untracked custody (e.g. an exchange),
-- listed so their basis can be used where they are eventually sold.
data Exported = Exported
  { exEvent   :: Text
  , exDate    :: Day
  , exAccount :: Text
  , exAsset   :: Text
  , exLot     :: Lot
  }

data Result = Result
  { resRows     :: [Row]
  , resIncome   :: [IncomeRow]
  , resIssues   :: [Issue]
  , resExported :: [Exported]
  , resHoldings :: Map Integer [((Text, Text), Lot)] -- ^ at each year end
  }

-- | An open perpetual position slice: size and the opening fee it carries.
data PLot = PLot
  { plSize :: Rational
  , plFee  :: Rational
  , plDate :: Day
  }

data St = St
  { book     :: !Book
  , perps    :: !(Map (Text, Text, Text) (Seq PLot)) -- ^ account, market, side
  , rows     :: [Row]
  , income   :: [IncomeRow]
  , funding  :: !(Map (Text, Text, Day) Rational)
  , exported :: [Exported]
  , issues   :: [Issue]
  , year     :: Maybe Integer
  , snaps    :: Map Integer [((Text, Text), Lot)]
  , symbols  :: Map Text Text
  }

run :: [Integer] -> [Event] -> Result
run years events =
  let st = foldl' step (St Map.empty Map.empty [] [] Map.empty [] [] Nothing Map.empty Map.empty) events
      st' = snapshotUntil (maximum (0 : years) + 1) st
      fundingRows =
        [ Row Unreported Short (market <> "-PERP funding") 0 (Just d) d (max 0 v) (max 0 (negate v)) acct "funding" 1
        | ((acct, market, d), v) <- Map.toList (funding st'), v /= 0 ]
  in Result
       { resRows = reverse (rows st') ++ fundingRows
       , resIncome = reverse (income st')
       , resIssues = reverse (issues st')
       , resExported = reverse (exported st')
       , resHoldings = snaps st'
       }

yearOf :: Day -> Integer
yearOf d = let (y, _, _) = toGregorian d in y

-- | Record holdings for every year that ended before year @y@ began.
snapshotUntil :: Integer -> St -> St
snapshotUntil y st = case year st of
  Nothing -> st { year = Just y }
  Just cur
    | cur >= y -> st
    | otherwise ->
        let held = Lot.holding (book st)
        in st { year = Just y, snaps = foldr (\yy -> Map.insert yy held) (snaps st) [cur .. y - 1] }

step :: St -> Event -> St
step st0 ev =
  let st = (snapshotUntil (yearOf (evDate ev)) st0)
             { symbols = foldr (\l -> Map.insert (legAsset l) (legSymbol l)) (symbols st0) (evLegs ev ++ evFees ev) }
  in payFees ev $ case evKind ev of
       Trade        -> trade ev st
       Move         -> move ev st
       Income       -> foldl' (receiveIncome ev) st (ins ev)
       Spend        -> foldl' (\s l -> dispose ev Unreported (legAccount l) (legAsset l) (qty l) (fmv l) s) (needPrice ev (outs ev) st) (outs ev)
       Add          -> add ev st
       Remove       -> foldl' (remove ev) st (outs ev)
       Perp pf      -> perp ev pf st
       Funding a m v -> st { funding = Map.insertWith (+) (a, m, evDate ev) v (funding st) }
       BrokerSale b -> broker ev b st

outs, ins :: Event -> [Leg]
outs ev = [ l | l <- evLegs ev, legAmount l < 0 ]
ins ev = [ l | l <- evLegs ev, legAmount l > 0 ]

qty :: Leg -> Rational
qty = abs . legAmount

-- | USD value of a leg; cash is worth its face amount.
usdOf :: Leg -> Maybe Rational
usdOf l
  | isCash (legAsset l) = Just (qty l)
  | otherwise = legUSD l

issue :: Event -> Bool -> Text -> St -> St
issue ev blocking msg st = st { issues = Issue (evId ev) blocking msg : issues st }

-- | Fair market value; 0 when unknown, which 'needPrice' has flagged.
fmv :: Leg -> Rational
fmv = fromMaybe 0 . usdOf

needPrice :: Event -> [Leg] -> St -> St
needPrice ev ls st = case [ l | l <- ls, isNothing (usdOf l) ] of
  [] -> st
  missing -> issue ev True ("no trusted USD price for " <> T.intercalate ", " (map legSymbol missing) <> "; valued at $0 provisionally") st

-- | Network fees are spent property: a disposal at fair market value. Their
-- value reduces a trade's proceeds (or adds to its basis) in 'trade'; for
-- every other event it is a nondeductible personal cost.
payFees :: Event -> St -> St
payFees ev st0 =
  let st = needPrice ev (evFees ev) st0
  in foldl' (\s l -> dispose ev Unreported (legAccount l) (legAsset l) (qty l) (fmv l) s) st (evFees ev)

-- | Split a total across legs in proportion to their USD values (or evenly
-- by count when a value is missing; that case already carries an issue).
allocate :: Rational -> [Leg] -> [(Leg, Rational)]
allocate _ [] = []
allocate total [l] = [(l, total)]
allocate total ls = case traverse usdOf ls of
  Just vs | sum vs > 0 -> [ (l, total * v / sum vs) | (l, v) <- zip ls vs ]
  _ -> [ (l, total / fromIntegral (length ls)) | l <- ls ]

-- | Exchange of property: one value V for both sides (§1.1001-1). A cash
-- side is exact and wins; otherwise the received side's value (the amount
-- realized), falling back to what was given up. Transaction costs reduce
-- proceeds when property is disposed of, else increase basis (§1.1001-7).
trade :: Event -> St -> St
trade ev st0 =
  let os = outs ev
      is = ins ev
      total = fmap sum . traverse usdOf
      cashOnly ls = not (null ls) && all (isCash . legAsset) ls
      (value, st1)
        | cashOnly os = (sum (map qty os), lopsided st0)
        | cashOnly is = (sum (map qty is), lopsided st0)
        | Just v <- total is = (v, lopsided st0)
        | Just v <- total os = (v, st0)
        | otherwise = (0, needPrice ev (os ++ is) st0)
      -- Both sides priced yet far apart: part of the exchange happened
      -- elsewhere (a cross-chain leg that did not pair), so V is wrong.
      lopsided s = case (total os, total is) of
        (Just o, Just i) | evReason ev /= "confirmed", o > 0, i > 0, min o i / max o i < 9 / 10 ->
          issue ev True ("the sides of this exchange are worth $" <> showQ (roundC o) <> " and $" <> showQ (roundC i)
                         <> "; part of it happened elsewhere (pair it with its other half, or override)") s
        _ -> s
      roundC x = fromInteger (round (x * 100)) / 100
      feeUSD = sum (mapMaybe usdOf (evFees ev))
      disposesProperty = any (not . isCash . legAsset) os
      proceeds = if disposesProperty then value - feeUSD else value
      basis = if disposesProperty then value else value + feeUSD
      st2 = if proceeds < 0 then issue ev False "fees exceed the traded value; proceeds floored at $0" st1 else st1
      st3 = foldl' (\s (l, p) -> dispose ev Unreported (legAccount l) (legAsset l) (qty l) (max 0 p) s) st2 (allocate proceeds os)
  in foldl' (\s (l, b) -> acquire ev (legAccount l) (legAsset l) (qty l) b s) st3 (allocate basis is)

acquire :: Event -> Text -> Text -> Rational -> Rational -> St -> St
acquire ev acct asset q cost st
  | isCash asset || q <= 0 = st
  | otherwise =
      let time = maybe (evTime ev) (\d -> UTCTime d 0) (evAcquired ev)
          date = fromMaybe (evDate ev) (evAcquired ev)
      in st { book = Lot.insert acct asset (Lot q cost time date (evId ev)) (book st) }

-- | Dispose of @q@ units FIFO for @proceeds@, writing one row per lot slice.
-- Units the book does not hold are missing history: they get a $0 basis
-- dated the sale day provisionally, and block a final report.
dispose :: Event -> Origin -> Text -> Text -> Rational -> Rational -> St -> St
dispose ev origin acct asset q proceeds st
  | isCash asset || q <= 0 = st
  | otherwise =
      let (slices, missing, book') = Lot.take acct asset q (book st)
          sold = evDate ev
          row l = Row origin (holdingPeriod (lotDate l) sold) (sym st asset) (lotQty l) (Just (lotDate l))
                      sold (proceeds * lotQty l / q) (lotCost l) acct (evId ev) 1
          gap = [ Row origin Short (sym st asset) missing (Just sold) sold (proceeds * missing / q) 0 acct (evId ev) 1 | missing > 0 ]
          st' = st { book = book', rows = reverse (gap ++ map row slices) ++ rows st }
      in if missing > 0 then issue ev True (missingText missing asset acct st) st' else st'

missingText :: Rational -> Text -> Text -> St -> Text
missingText q asset acct st =
  "missing history: " <> showQ q <> " " <> sym st asset <> " not held in " <> acct
    <> " (provisional basis $0); find the acquisition or override with its basis"

sym :: St -> Text -> Text
sym st asset = Map.findWithDefault asset asset (symbols st)

-- | Transfers between own accounts carry lots with their original dates.
-- What the sender lost beyond what arrived (bridge or relayer fee) is spent
-- at fair market value; extra arriving units have no known basis ($0).
-- Units the sender never held are missing history, unless the event says the
-- account is only partly observed ("unseen_at_fmv"): then they arrive with
-- their fair market value as basis.
move :: Event -> St -> St
move ev st0 = foldl' perAsset st0 assets
  where
    assets = Map.toList (Map.fromListWith (flip (++)) [ (legAsset l, [l]) | l <- evLegs ev, not (isCash (legAsset l)) ])
    perAsset st (asset, ls) =
      let froms = [ (legAccount l, qty l, l) | l <- ls, legAmount l < 0 ]
          tos = [ (legAccount l, qty l) | l <- ls, legAmount l > 0 ]
      in go asset froms tos st
    go asset ((a, qa, l) : fs) ((b, qb) : ts) st =
      let q = min qa qb
          fs' = if qa > q then (a, qa - q, l) : fs else fs
          ts' = if qb > q then (b, qb - q) : ts else ts
      -- Out and back within one account (a sponsor lending gas) moves nothing.
      in go asset fs' ts' (if a == b then st else carry asset a b q l st)
    go asset ((a, qa, l) : fs) [] st =
      let value = fmv l * qa / qty l
      in go asset fs [] (dispose ev Unreported a asset qa value (needPrice ev [l] st))
    go asset [] ((b, qb) : ts) st =
      let st' = issue ev False ("received " <> showQ qb <> " " <> sym st asset <> " more than was sent; extra units get $0 basis") st
      in go asset [] ts (acquire ev b asset qb 0 st')
    go _ [] [] st = st
    carry asset a b q l st =
      let (slices, missing, book') = Lot.take a asset q (book st)
          atFMV = evReason ev == "unseen_at_fmv"
          filler = [ Lot missing (if atFMV then fmv l * missing / qty l else 0) (evTime ev) (evDate ev) (evId ev) | missing > 0 ]
          st' = st { book = foldr (Lot.insert b asset) book' (slices ++ filler) }
      in if
           | missing == 0 -> st'
           | atFMV -> issue ev False (showQ missing <> " " <> sym st asset <> " sent from " <> a <> " were never seen arriving there; basis = value at transfer") (needPrice ev [l] st')
           | otherwise -> issue ev True (missingText missing asset a st) st'

receiveIncome :: Event -> St -> Leg -> St
receiveIncome ev st0 l =
  let st = needPrice ev [l] st0
      v = fmv l
      row = IncomeRow (evDate ev) (evReason ev) (legSymbol l) (qty l) v (legAccount l) (evId ev)
  in acquire ev (legAccount l) (legAsset l) (qty l) v st { income = row : income st }

-- | Property arriving without a purchase or income event: spam ($0 basis),
-- gifts (donor's basis and date), own untracked custody (stated basis or
-- fair market value).
add :: Event -> St -> St
add ev st0 = case evBasis ev of
  Just b -> foldl' (\s (l, c) -> acquire ev (legAccount l) (legAsset l) (qty l) c s) st0 (allocate b (ins ev))
  Nothing ->
    let st = needPrice ev (ins ev) st0
    in foldl' (\s l -> acquire ev (legAccount l) (legAsset l) (qty l) (fmv l) s) st (ins ev)

remove :: Event -> St -> Leg -> St
remove ev st l
  | isCash (legAsset l) = st
  | otherwise =
      let (slices, missing, book') = Lot.take (legAccount l) (legAsset l) (qty l) (book st)
          ex = [ Exported (evId ev) (evDate ev) (legAccount l) (legAsset l) s | evReason ev == "custody", s <- slices ]
          st' = st { book = book', exported = reverse ex ++ exported st }
      in if missing > 0 then issue ev True (missingText missing (legAsset l) (legAccount l) st) st' else st'

-- | Perpetual futures are capital assets here (not §1256 contracts): each
-- close realizes the exchange-reported PnL less fees, with a holding period
-- from FIFO position slices. Fees of a fill are shared by size between the
-- part that closes and the part that opens.
perp :: Event -> PerpFill -> St -> St
perp ev pf st0 =
  let size = maybe 0 ppSize
      total = size (pfClose pf) + size (pfOpen pf)
      closeFee = if total == 0 then 0 else pfFee pf * size (pfClose pf) / total
      openFee = pfFee pf - closeFee
      key side = (pfAccount pf, pfMarket pf, side)
      st1 = case pfOpen pf of
        Just (PerpPart side s) ->
          st0 { perps = Map.insertWith (flip (<>)) (key side) (Seq.singleton (PLot s openFee (evDate ev))) (perps st0) }
        Nothing -> st0
  in case pfClose pf of
       Nothing -> st1
       Just (PerpPart side s) ->
         let (slices, missing, rest) = takePerp s (Map.findWithDefault Seq.empty (key side) (perps st1))
             net = pfPnL pf - closeFee
             label = pfMarket pf <> "-PERP " <> side
             row l =
               let g = net * plSize l / s - plFee l
               in Row Unreported (holdingPeriod (plDate l) (evDate ev)) label (plSize l) (Just (plDate l)) (evDate ev)
                      (max 0 g) (max 0 (negate g)) (pfAccount pf) (evId ev) 1
             gap = [ PLot missing 0 (evDate ev) | missing > 0 ]
             st2 = st1 { perps = Map.insert (key side) rest (perps st1)
                       , rows = reverse (map row (slices ++ gap)) ++ rows st1 }
         in if missing > 0
              then issue ev True ("closing " <> showQ missing <> " " <> label <> " that was never opened in the fetched history") st2
              else st2

takePerp :: Rational -> Seq PLot -> ([PLot], Rational, Seq PLot)
takePerp = go []
  where
    go acc need lots
      | need <= 0 = (reverse acc, 0, lots)
      | otherwise = case Seq.viewl lots of
          Seq.EmptyL -> (reverse acc, need, Seq.empty)
          l Seq.:< rest
            | plSize l <= need -> go (l : acc) (need - plSize l) rest
            | otherwise ->
                let part = l { plSize = need, plFee = plFee l * need / plSize l }
                in (reverse (part : acc), 0, l { plSize = plSize l - need, plFee = plFee l - plFee part } Seq.<| rest)

-- | A Form 1099-DA row. When the broker states its own basis (units it sold
-- from its own purchases), the row is reported as the broker printed it.
-- Otherwise the units came from elsewhere and are taken from the account's
-- lots, which arrived through moves.
broker :: Event -> Broker -> St -> St
broker ev b st
  | isCash (brAsset b) =
      passthrough (fromMaybe (brProceeds b) (brBasis b)) (fromMaybe Short (brTerm b)) Nothing st
  | Just basis <- brBasis b = case (evAcquired ev, brTerm b) of
      (Just d, _)   -> passthrough basis (holdingPeriod d (evDate ev)) (Just d) st
      (_, Just t)   -> passthrough basis t Nothing st
      (Nothing, Nothing) -> issue ev True "broker basis without a holding period; override with basis and acquired"
                              (passthrough basis Short Nothing st)
  | otherwise = dispose ev origin (brAccount b) (brAsset b) (brAmount b) (brProceeds b) st
  where
    origin = BrokerReported (brBasisReported b)
    passthrough basis term acquired s =
      s { rows = Row origin term (brSymbol b) (brAmount b) acquired (evDate ev) (brProceeds b) basis
                     (brAccount b) (evId ev) 1 : rows s }

showQ :: Rational -> Text
showQ r =
  let scaled = round (abs r * 10 ^ (18 :: Int)) :: Integer
      (w, f) = scaled `divMod` (10 ^ (18 :: Int))
      frac = T.dropWhileEnd (== '0') (T.justifyRight 18 '0' (T.pack (show f)))
  in (if r < 0 then "-" else "") <> T.pack (show w) <> (if T.null frac then "" else "." <> frac)
