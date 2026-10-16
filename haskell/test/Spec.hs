{-# LANGUAGE OverloadedStrings #-}

module Main (main) where

import           Control.Monad   (unless, when)
import           Data.IORef
import qualified Data.Map.Strict as Map
import           Data.Text       (Text)
import qualified Data.Text       as T
import           Data.Time
import           System.Exit     (exitFailure)
import           Test.QuickCheck

import           Engine
import           Lot             (Lot (..))
import           Report
import           Types

-- ── Builders ────────────────────────────────────────────────────────────────

at :: Integer -> Int -> Int -> UTCTime
at y m d = UTCTime (fromGregorian y m d) 43200

ev :: Text -> UTCTime -> Kind -> [Leg] -> [Leg] -> Event
ev i t k ls fs = Event i t (utctDay t) k ls fs "" Nothing Nothing ""

leg :: Text -> Text -> Rational -> Maybe Rational -> Leg
leg acct asset amt usd = Leg acct asset asset amt usd

buy :: Text -> UTCTime -> Text -> Rational -> Rational -> Event
buy i t asset q cost = ev i t Trade [leg "a" "USD" (negate cost) Nothing, leg "a" asset q Nothing] []

sell :: Text -> UTCTime -> Text -> Rational -> Rational -> Event
sell i t asset q got = ev i t Trade [leg "a" asset (negate q) Nothing, leg "a" "USD" got Nothing] []

rowsOf :: [Event] -> [Row]
rowsOf = resRows . run [2024, 2025, 2026]

blocking :: [Event] -> [Issue]
blocking = filter isBlocking . resIssues . run [2024, 2025, 2026]

-- ── Tiny test harness ───────────────────────────────────────────────────────

type Check = IORef Int

expect :: Check -> String -> Bool -> IO ()
expect failures name ok = unless ok $ do
  putStrLn ("FAIL: " ++ name)
  modifyIORef failures (+ 1)

eq :: (Eq a, Show a) => Check -> String -> a -> a -> IO ()
eq failures name want got =
  expect failures (name ++ "\n  want " ++ show want ++ "\n  got  " ++ show got) (want == got)

amounts :: Row -> (Rational, Rational, Rational, Term, Maybe Day)
amounts r = (rAmount r, rProceeds r, rBasis r, rTerm r, rAcquired r)

main :: IO ()
main = do
  failures <- newIORef 0

  -- Holding period: more than one year, on calendar dates.
  eq failures "one year to the day is short-term" Short (holdingPeriodOf (fromGregorian 2024 1 10) (fromGregorian 2025 1 10))
  eq failures "a year and a day is long-term" Long (holdingPeriodOf (fromGregorian 2024 1 10) (fromGregorian 2025 1 11))
  eq failures "Feb 29 rolls to Mar 1" Short (holdingPeriodOf (fromGregorian 2024 2 29) (fromGregorian 2025 3 1))
  eq failures "Feb 29 long after Mar 1" Long (holdingPeriodOf (fromGregorian 2024 2 29) (fromGregorian 2025 3 2))

  -- Sale for cash: the cash side values the exchange; fees reduce proceeds.
  let saleFee = [ buy "b" (at 2024 1 10) "SOL" 1 100
                , ev "s" (at 2025 1 11) Trade [leg "a" "SOL" (-1) (Just 150), leg "a" "USD" 148 Nothing] [leg "a" "USD" 2 Nothing] ]
  eq failures "sale: proceeds net of fee, long-term"
     [(1, 146, 100, Long, Just (fromGregorian 2024 1 10))] (map amounts (rowsOf saleFee))

  -- Purchase for cash: the fee is added to basis.
  let buyFee = [ ev "b" (at 2025 1 1) Trade [leg "a" "USD" (-100) Nothing, leg "a" "SOL" 1 (Just 99)] [leg "a" "USD" 1 Nothing]
               , sell "s" (at 2025 2 1) "SOL" 1 120 ]
  eq failures "purchase: fee capitalized" [(1, 120, 101, Short, Just (fromGregorian 2025 1 1))] (map amounts (rowsOf buyFee))

  -- Crypto-for-crypto: received side values it; fee in the disposed asset
  -- is itself a disposal and reduces proceeds.
  let swap = [ buy "b" (at 2025 1 1) "SOL" 2 100
             , ev "x" (at 2025 1 2) Trade [leg "a" "SOL" (-1) (Just 100), leg "a" "TOK" 50 (Just 98)] [leg "a" "SOL" (1/100) (Just 1)]
             , sell "s" (at 2025 1 3) "TOK" 50 60 ]
  eq failures "swap: SOL out at V - fee, fee SOL disposed at FMV, TOK basis = V"
     [ (1, 97, 50, Short, Just (fromGregorian 2025 1 1))
     , (1/100, 1, 1/2, Short, Just (fromGregorian 2025 1 1))
     , (50, 60, 98, Short, Just (fromGregorian 2025 1 2)) ]
     (map amounts (rowsOf swap))

  -- FIFO is by original acquisition date across a move (§1.1012-1(j)).
  let fifo = [ ev "a1" (at 2024 3 1) Trade [leg "A" "USD" (-10) Nothing, leg "A" "ETH" 1 Nothing] []
             , ev "b1" (at 2024 2 1) Trade [leg "B" "USD" (-20) Nothing, leg "B" "ETH" 1 Nothing] []
             , ev "m" (at 2024 4 1) Move [leg "A" "ETH" (-1) Nothing, leg "B" "ETH" 1 Nothing] []
             , ev "s" (at 2024 5 1) Trade [leg "B" "ETH" (-1) Nothing, leg "B" "USD" 30 Nothing] [] ]
  eq failures "move keeps dates; FIFO takes the earliest acquired" [(1, 30, 20, Short, Just (fromGregorian 2024 2 1))]
     (map amounts (rowsOf (sortByTime fifo)))

  -- Bridge shortfall is spent at fair market value; the rest keeps its basis.
  let bridge = [ ev "b" (at 2025 1 1) Trade [leg "A" "USD" (-1000) Nothing, leg "A" "ETH" 1 Nothing] []
               , ev "m" (at 2025 1 2) Move [leg "A" "ETH" (-1) (Just 2000), leg "B" "ETH" (99/100) (Just 1980)] []
               , ev "s" (at 2025 1 3) Trade [leg "B" "ETH" (-99/100) Nothing, leg "B" "USD" 2000 Nothing] [] ]
  eq failures "bridge: shortfall spent, remainder carried"
     [ (1/100, 20, 10, Short, Just (fromGregorian 2025 1 1))
     , (99/100, 2000, 990, Short, Just (fromGregorian 2025 1 1)) ]
     (map amounts (rowsOf bridge))

  -- A partly observed account: unseen units arrive at fair market value.
  let unseen = [ (ev "w" (at 2025 1 1) Move [leg "rh" "SOL" (-2) (Just 300), leg "a" "SOL" 2 (Just 300)] []) { evReason = "unseen_at_fmv" }
               , sell "s" (at 2025 1 2) "SOL" 2 310 ]
  eq failures "unseen units: FMV basis" [(2, 310, 300, Short, Just (fromGregorian 2025 1 1))] (map amounts (rowsOf unseen))
  eq failures "unseen units: not blocking" 0 (length (blocking unseen))

  -- Missing history blocks and is provisionally $0 basis.
  let missing = [sell "s" (at 2025 1 2) "SOL" 1 50]
  eq failures "missing history row" [(1, 50, 0, Short, Just (fromGregorian 2025 1 2))] (map amounts (rowsOf missing))
  eq failures "missing history blocks" 1 (length (blocking missing))

  -- Lopsided exchange blocks unless confirmed.
  let lop = [ buy "b" (at 2025 1 1) "SOL" 1 20
            , ev "x" (at 2025 1 2) Trade [leg "a" "SOL" (-1) (Just 22), leg "a" "USD" (1/20) Nothing] [] ]
  eq failures "lopsided trade blocks" 1 (length (blocking lop))
  eq failures "confirmed lopsided trade does not" 0
     (length (blocking [ if evId e == "x" then e { evReason = "confirmed" } else e | e <- lop ]))

  -- Income: FMV is ordinary income and basis.
  let inc = [ (ev "i" (at 2025 1 1) Income [leg "a" "TOK" 100 (Just 50)] []) { evReason = "airdrop" }
            , sell "s" (at 2025 6 1) "TOK" 100 80 ]
  eq failures "income amount" [50] (map iUSD (resIncome (run [2025] inc)))
  eq failures "income basis" [(100, 80, 50, Short, Just (fromGregorian 2025 1 1))] (map amounts (rowsOf inc))

  -- Spam: $0 basis.
  let spam = [ (ev "i" (at 2025 1 1) Add [leg "a" "JUNK" 7 Nothing] []) { evReason = "spam", evBasis = Just 0 }
             , sell "s" (at 2025 1 5) "JUNK" 7 5 ]
  eq failures "spam basis zero" [(7, 5, 0, Short, Just (fromGregorian 2025 1 1))] (map amounts (rowsOf spam))

  -- Perpetuals: fees of open and close reduce the realized gain.
  let perp i t c o pnl fee = ev i t (Perp (PerpFill "hl" "BTC" c o pnl fee)) [] []
      long n = Just (PerpPart "long" n)
      short n = Just (PerpPart "short" n)
      perps = [ perp "o" (at 2025 1 1) Nothing (long 1) 0 4
              , perp "f" (at 2025 2 1) (long 1) (short 1) 100 8
              , perp "c" (at 2025 3 1) (short 1) Nothing (-50) 2 ]
  eq failures "perp flip: close long nets both fees; short close carries its open fee"
     [ (1, 92, 0, Short, Just (fromGregorian 2025 1 1))
     , (1, 0, 56, Short, Just (fromGregorian 2025 2 1)) ]
     (map amounts (rowsOf perps))

  -- Funding nets per market per day.
  let fund = [ ev "f1" (at 2025 1 1) (Funding "hl" "BTC" 3) [] []
             , ev "f2" (at 2025 1 1) (Funding "hl" "BTC" (-5)) [] [] ]
  eq failures "funding nets to one loss row" [(0, 0, 2, Short, Just (fromGregorian 2025 1 1))] (map amounts (rowsOf fund))

  -- Broker rows: the broker's own basis passes through; cash is at par.
  let br b = ev "r" (at 2025 9 15) (BrokerSale b) [] []
      passed = rowsOf [br (Broker "rh" "SOL" "SOL" 2 441 (Just 412) (Just Short) False)]
  eq failures "broker passthrough" [(2, 441, 412, Short, Nothing)] (map amounts passed)
  eq failures "broker passthrough origin" [BrokerReported False] (map rOrigin passed)
  eq failures "broker stablecoin at par" [(495, 495, 495, Short, Nothing)]
     (map amounts (rowsOf [br (Broker "rh" "USD" "USD" 495 495 Nothing Nothing False)]))

  -- Form 8949 boxes.
  let r o t = Row o t "X" 1 Nothing (fromGregorian 2025 1 1) 1 1 "a" "e" 1
  eq failures "2024 unreported short is C" C (box 2024 (r Unreported Short))
  eq failures "2025 unreported short is I" I (box 2025 (r Unreported Short))
  eq failures "2025 broker without basis long is K" K (box 2025 (r (BrokerReported False) Long))
  eq failures "2025 broker with basis short is G" G (box 2025 (r (BrokerReported True) Short))

  -- Rounding: half away from zero.
  eq failures "cents" [1, -1, 0, 100] (map cents [5 / 1000, -5 / 1000, 49 / 10000, 1])

  -- Consolidation keeps exact sums.
  let c1 = r Unreported Short
      merged = consolidate [c1 { rProceeds = 1 / 3 }, c1 { rProceeds = 2 / 3 }]
  eq failures "consolidate sums" [(2, 1, 2, Short, Nothing)] (map amounts merged)

  -- Properties.
  let prop name p = do
        res <- quickCheckWithResult stdArgs { chatty = False, maxSuccess = 300 } p
        case res of
          Success {} -> pure ()
          _          -> expect failures name False
  prop "basis and quantity are conserved" prop_conservation
  prop "rows are never negative" prop_nonNegative

  n <- readIORef failures
  when (n > 0) $ putStrLn (show n ++ " test(s) failed") >> exitFailure
  putStrLn "all tests passed"

holdingPeriodOf :: Day -> Day -> Term
holdingPeriodOf a s =
  let res = rowsOf [ buy "b" (UTCTime a 0) "SOL" 1 1, sell "s" (UTCTime s 0) "SOL" 1 1 ]
  in case res of
       [x] -> rTerm x
       _   -> error "expected one row"

sortByTime :: [Event] -> [Event]
sortByTime = map snd . Map.toList . Map.fromList . map (\e -> (evTime e, e))

-- Random buys and sells in one account (sells never exceed holdings).
newtype History = History [Event]

instance Show History where
  show (History es) = show (map evId es)

instance Arbitrary History where
  arbitrary = do
    n <- choose (1, 40)
    History . reverse . snd <$> go n (0 :: Rational) (0 :: Int) []
    where
      go :: Int -> Rational -> Int -> [Event] -> Gen (Rational, [Event])
      go 0 held _ acc = pure (held, acc)
      go k held i acc = do
        isBuy <- if held == 0 then pure True else arbitrary
        let t = addUTCTime (fromIntegral i * 86400 * 13) (at 2024 1 1)
            name = T.pack (show i)
        if isBuy
          then do
            q <- (% 1000) <$> choose (1, 5000)
            c <- (% 100) <$> choose (0, 1000000)
            go (k - 1) (held + q) (i + 1) (buy name t "SOL" q c : acc)
          else do
            frac <- (% 7) <$> choose (1, 7)
            p <- (% 100) <$> choose (0, 1000000)
            let q = held * frac
            go (k - 1) (held - q) (i + 1) (sell name t "SOL" q p : acc)
      (%) a b = fromInteger a / fromInteger b

prop_conservation :: History -> Bool
prop_conservation (History evs) =
  let res = run [2024, 2025, 2026] evs
      bought = sum [ c | e <- evs, Trade <- [evKind e], [Leg _ "USD" _ a _, Leg _ "SOL" _ _ _] <- [evLegs e], let c = negate a ]
      qIn = sum [ q | e <- evs, [Leg _ "USD" _ _ _, Leg _ "SOL" _ q _] <- [evLegs e] ]
      qOut = sum (map rAmount (resRows res))
      final = Map.findWithDefault [] 2026 (resHoldings res)
      left = sum (map (lotCost . snd) final)
      qLeft = sum (map (lotQty . snd) final)
  in bought == sum (map rBasis (resRows res)) + left && qIn == qOut + qLeft && null (resIssues res)

prop_nonNegative :: History -> Bool
prop_nonNegative (History evs) = all (\r -> rProceeds r >= 0 && rBasis r >= 0) (resRows (run [2024] evs))
