{-# LANGUAGE OverloadedStrings    #-}
{-# LANGUAGE ScopedTypeVariables  #-}

module Main (main) where

import           Control.Exception  (ErrorCall(..), evaluate, try)
import           Control.Monad      (unless)
import           Test.QuickCheck
import qualified Data.Aeson      as Aeson
import qualified Data.ByteString.Lazy as BL
import qualified Data.Map.Strict as Map
import           Data.Ratio      ((%))
import           Data.Text       (Text)
import qualified Data.Text       as T
import qualified Data.Text.IO    as TIO
import           Data.Time       (Day, UTCTime(..), addDays, fromGregorian,
                                  secondsToDiffTime, toGregorian)
import           Paths_cryptotax_core (getDataFileName)
import           System.Exit     (exitFailure)
import           Types
import           Lot             (LotQueue)
import qualified Lot
import           GainLoss        (processTransactions, ProcessResult(..))
import           Report          (render8949CSV, renderIncomeCSV, renderPerpPnlCSV,
                                  renderTransfersCSV)

-- ---------------------------------------------------------------------------
-- Generators
-- ---------------------------------------------------------------------------

genPositiveAmount :: Gen Rational
genPositiveAmount = do
  n <- choose (1, 100000 :: Integer)
  d <- choose (1, 10000 :: Integer)
  pure (n % d)

genPrice :: Gen Rational
genPrice = do
  n <- choose (1, 50000 :: Integer)
  d <- choose (1, 100 :: Integer)
  pure (n % d)

genTime :: Gen UTCTime
genTime = do
  year  <- choose (2020, 2025)
  month <- choose (1, 12)
  day   <- choose (1, 28)
  secs  <- choose (0, 86399)
  pure $ UTCTime (fromGregorian year month day) (secondsToDiffTime secs)

genAsset :: Gen AssetSymbol
genAsset = elements ["ETH", "BTC", "SOL", "USDC", "DOGE"]

-- | Generate buys and a sell amount guaranteed <= total bought.
genBuysAndSell :: Gen (AssetSymbol, [(Rational, Rational)], Rational)
genBuysAndSell = do
  asset <- genAsset
  n <- choose (1, 5)
  buys <- vectorOf n $ (,) <$> genPositiveAmount <*> genPrice
  let totalBought = sum (map fst buys)
  fraction <- frequency
    [ (1, pure (100 :: Integer))
    , (4, choose (1, 99 :: Integer))
    ]
  let sellAmt = totalBought * (fraction % 100)
  pure (asset, buys, sellAmt)

-- | Build a lot queue from a list of (amount, price) buys.
buildQueue :: AssetSymbol -> UTCTime -> [(Rational, Rational)] -> LotQueue
buildQueue asset t buys =
  foldl (\q (amt, px) ->
    Lot.acquire asset t (TokenAmount amt) (USD (amt * px)) q) Lot.empty buys

-- | Integer-amount text for integration tests that go through parseDecimal.
showI :: Integer -> Text
showI = T.pack . show

-- ---------------------------------------------------------------------------
-- 1. Conservation of units: acquired - disposed = remaining
-- ---------------------------------------------------------------------------

prop_conservationOfUnits :: Property
prop_conservationOfUnits = forAll genBuysAndSell $ \(asset, buys, sellAmt) ->
  let totalBought = sum (map fst buys)
      t0  = UTCTime (fromGregorian 2024 1 1) 0
      t1  = UTCTime (fromGregorian 2024 6 1) 0
      queue = buildQueue asset t0 buys
      disp  = Disposal asset t1 (TokenAmount sellAmt) (USD (sellAmt * 100)) 0
  in cover 15 (sellAmt == totalBought) "full disposal" $
     cover 40 (sellAmt < totalBought) "partial disposal" $
     case Lot.dispose disp queue of
       Left _  -> property False
       Right (_, queue') ->
         Lot.totalUnits asset queue' === TokenAmount (totalBought - sellAmt)

-- ---------------------------------------------------------------------------
-- 2. Conservation of cost basis: remaining + disposed = total spent
-- ---------------------------------------------------------------------------

prop_conservationOfCostBasis :: Property
prop_conservationOfCostBasis = forAll genBuysAndSell $ \(asset, buys, sellAmt) ->
  let totalCost = sum [amt * px | (amt, px) <- buys]
      t0  = UTCTime (fromGregorian 2024 1 1) 0
      t1  = UTCTime (fromGregorian 2024 6 1) 0
      queue = buildQueue asset t0 buys
      disp  = Disposal asset t1 (TokenAmount sellAmt) (USD (sellAmt * 200)) 0
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (gains, queue') ->
         let disposedBasis  = sum (map (unUSD . glCostBasis) gains)
             remainingBasis = unUSD (Lot.totalCostBasis queue')
         in property (disposedBasis + remainingBasis == totalCost)

-- ---------------------------------------------------------------------------
-- 3. Non-negative lots after disposal
-- ---------------------------------------------------------------------------

prop_nonNegativeLots :: Property
prop_nonNegativeLots = forAll genBuysAndSell $ \(asset, buys, sellAmt) ->
  let t0  = UTCTime (fromGregorian 2024 1 1) 0
      t1  = UTCTime (fromGregorian 2024 6 1) 0
      queue = buildQueue asset t0 buys
      disp  = Disposal asset t1 (TokenAmount sellAmt) (USD (sellAmt * 100)) 0
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (_, queue') ->
         let allLots = concatMap snd (Map.toList queue')
         in property $ all (\l -> unTokens (lotRemaining l) >= 0) allLots

-- ---------------------------------------------------------------------------
-- 4. FIFO ordering: older lots consumed first
-- ---------------------------------------------------------------------------

prop_fifoOrdering :: Property
prop_fifoOrdering = forAll genPositiveAmount $ \amt ->
  let t1 = UTCTime (fromGregorian 2023 1 1) 0  -- older
      t2 = UTCTime (fromGregorian 2024 1 1) 0  -- newer
      t3 = UTCTime (fromGregorian 2024 6 1) 0  -- disposal
      queue = Lot.acquire "ETH" t2 (TokenAmount amt) (USD (amt * 3000))
            $ Lot.acquire "ETH" t1 (TokenAmount amt) (USD (amt * 2000)) Lot.empty
      disp = Disposal "ETH" t3 (TokenAmount amt) (USD (amt * 2500)) 0
  in case Lot.dispose disp queue of
       Left _       -> property False
       Right (gs, _) -> property $ all (\g -> glAcquired g == t1) gs

-- ---------------------------------------------------------------------------
-- 5. Disposing 0 units is a no-op
-- ---------------------------------------------------------------------------

prop_zeroDisposalNoop :: Property
prop_zeroDisposalNoop = forAll genPositiveAmount $ \amt ->
  forAll genPrice $ \px ->
    let t = UTCTime (fromGregorian 2024 1 1) 0
        queue = Lot.acquire "BTC" t (TokenAmount amt) (USD (amt * px)) Lot.empty
        disp  = Disposal "BTC" t 0 0 0
    in case Lot.dispose disp queue of
         Left _  -> property False
         Right (gains, queue') ->
           property (null gains && Lot.totalUnits "BTC" queue' == TokenAmount amt)

-- ---------------------------------------------------------------------------
-- 6. Gain formula: glGain == glProceeds - glCostBasis exactly
-- ---------------------------------------------------------------------------

prop_gainFormula :: Property
prop_gainFormula = forAll genBuysAndSell $ \(asset, buys, sellAmt) ->
  let t0  = UTCTime (fromGregorian 2024 1 1) 0
      t1  = UTCTime (fromGregorian 2024 6 1) 0
      queue = buildQueue asset t0 buys
      disp  = Disposal asset t1 (TokenAmount sellAmt) (USD (sellAmt * 200)) 0
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (gains, _) ->
         cover 30 (length gains > 1) "multi-lot disposal" $
         property $ all (\g -> glGain g == glProceeds g - glCostBasis g) gains

-- ---------------------------------------------------------------------------
-- 7. Holding period: IRS more-than-one-year calendar rule
-- ---------------------------------------------------------------------------

-- | Independent oracle for the IRS rule: long-term requires the disposal date
-- to be strictly after the one-year anniversary calendar date. The only
-- non-existent anniversary (Feb 29) maps to Mar 1, matching RollOver.
oracleHoldingPeriod :: Day -> Day -> HoldingPeriod
oracleHoldingPeriod acq sold =
  let (y, m, d) = toGregorian acq
      anniversary = if m == 2 && d == 29
                      then fromGregorian (y + 1) 3 1
                      else fromGregorian (y + 1) m d
  in if sold > anniversary then LongTerm else ShortTerm

prop_holdingPeriodCalendarRule :: Property
prop_holdingPeriodCalendarRule = forAll genAcqDay $ \acqDay ->
  forAll genOffset $ \offset ->
    let soldDay  = addDays offset acqDay
        buyTime  = UTCTime acqDay 0
        sellTime = UTCTime soldDay 0
        queue    = Lot.acquire "ETH" buyTime (TokenAmount 1) (USD 1000) Lot.empty
        disp     = Disposal "ETH" sellTime (TokenAmount 1) (USD 1200) 0
        expected = oracleHoldingPeriod acqDay soldDay
    in cover 30 (expected == LongTerm) "long-term" $
       cover 30 (expected == ShortTerm) "short-term" $
       case Lot.dispose disp queue of
         Left _  -> property False
         Right (gains, _) ->
           not (null gains) .&&. conjoin [glPeriod g === expected | g <- gains]
  where
    genAcqDay = frequency
      [ (3, do year  <- choose (2019, 2025)
               month <- choose (1, 12)
               day   <- choose (1, 28)
               pure (fromGregorian year month day))
      -- Leap-adjacent acquisition dates exercise the RollOver edge.
      , (1, elements [ fromGregorian 2024 2 29, fromGregorian 2024 2 28
                     , fromGregorian 2024 3 1,  fromGregorian 2020 2 29
                     , fromGregorian 2023 2 28, fromGregorian 2023 12 31 ])
      ]
    genOffset = frequency
      [ (3, choose (355, 375 :: Integer))  -- dense around the anniversary
      , (1, choose (1, 354))
      , (1, choose (376, 1200))
      ]

-- | Explicit anniversary cases: a sale on the one-year anniversary calendar
-- date is short-term, even across leap years (366 elapsed days).
prop_holdingPeriodAnniversaryCases :: Property
prop_holdingPeriodAnniversaryCases = once $ conjoin
  [ hp 2024 1 15 2025 1 15 === ShortTerm  -- anniversary date itself
  , hp 2024 1 15 2025 1 16 === LongTerm
  , hp 2023 6 1  2024 6 1  === ShortTerm  -- 366 elapsed days across leap year
  , hp 2023 6 1  2024 6 2  === LongTerm
  , hp 2024 2 29 2025 3 1  === ShortTerm  -- Feb 29 rolls over to Mar 1
  , hp 2024 2 29 2025 3 2  === LongTerm
  ]
  where
    hp y1 m1 d1 y2 m2 d2 =
      Lot.holdingPeriod (UTCTime (fromGregorian y1 m1 d1) 0)
                        (UTCTime (fromGregorian y2 m2 d2) 0)

-- ---------------------------------------------------------------------------
-- 8. No phantom gains on empty input
-- ---------------------------------------------------------------------------

prop_noPhantomGains :: Property
prop_noPhantomGains = once $
  let result = processTransactions []
  in property (null (prGainLosses result) && null (prIncome result))

-- ---------------------------------------------------------------------------
-- 9. Buy-then-sell roundtrip: gain = N * (sellPx - buyPx) exactly
-- ---------------------------------------------------------------------------

prop_buyThenSellExact :: Property
prop_buyThenSellExact = forAll genScenario $ \(amt, buyPx, sellPx) ->
  let buyTime  = UTCTime (fromGregorian 2024 1 1) 0
      sellTime = UTCTime (fromGregorian 2024 6 1) 0
      cost     = USD (amt * buyPx)
      proceeds = USD (amt * sellPx)
      expected = USD (amt * (sellPx - buyPx))
      queue    = Lot.acquire "ETH" buyTime (TokenAmount amt) cost Lot.empty
      disp     = Disposal "ETH" sellTime (TokenAmount amt) proceeds 0
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (gains, _) -> sum (map glGain gains) === expected
  where
    genScenario = (,,) <$> genPositiveAmount <*> genPrice <*> genPrice

-- ---------------------------------------------------------------------------
-- 10. Swap decomposition: one disposal + one acquisition
-- ---------------------------------------------------------------------------

prop_swapDecomposition :: Property
prop_swapDecomposition = forAll genSwapData $ \(sentAmt, sentPx, rcvAmt, rcvPx) ->
  let t1 = UTCTime (fromGregorian 2024 1 1) 0
      t2 = UTCTime (fromGregorian 2024 6 1) 0
      buyTx = Transaction
        { txId = "buy1", txTimestamp = t1, txSource = "test", txChain = "test"
        , txType = Buy, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "ETH" Nothing (showI sentAmt) (showI (sentAmt * sentPx)))
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      swapTx = Transaction
        { txId = "swap1", txTimestamp = t2, txSource = "test", txChain = "test"
        , txType = Swap, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" Nothing (showI sentAmt) (showI (sentAmt * sentPx)))
        , txReceived = Just (AssetAmount "SOL" Nothing (showI rcvAmt) (showI (rcvAmt * rcvPx)))
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [buyTx, swapTx]
  in conjoin
       [ property (length (prGainLosses result) >= 1)
       , Lot.totalUnits "SOL" (prFinalQueue result) === TokenAmount (fromIntegral rcvAmt)
       , Lot.totalUnits "ETH" (prFinalQueue result) === 0
       ]
  where
    genSwapData = (,,,) <$> choose (1, 100 :: Integer)
                        <*> choose (1, 5000 :: Integer)
                        <*> choose (1, 100 :: Integer)
                        <*> choose (1, 5000 :: Integer)

-- ---------------------------------------------------------------------------
-- 11. JSON roundtrip: encode then decode = identity
-- ---------------------------------------------------------------------------

prop_jsonRoundtrip :: Property
prop_jsonRoundtrip = forAll genTxPayload $ \payload ->
  Aeson.eitherDecode (Aeson.encode payload) === Right payload
  where
    genTxPayload = do
      n <- choose (0, 5)
      txs <- vectorOf n genTransaction
      pure $ TxPayload "1.0.0" ["0xabc", "0xdef"] txs

    genTransaction = do
      txId' <- T.pack . ("tx" ++) . show <$> (choose (1, 10000) :: Gen Int)
      ts <- genTime
      txType' <- elements [Buy, Sell, Swap, TransferIn, TransferOut, Income, FundingPayment, PerpOpen, PerpClose]
      sent <- case txType' of
        Sell      -> Just <$> genAssetAmount
        Swap      -> Just <$> genAssetAmount
        TransferOut -> Just <$> genAssetAmount
        PerpClose -> Just <$> genAssetAmount
        _ -> pure Nothing
      rcv <- case txType' of
        Buy            -> Just <$> genAssetAmount
        Swap           -> Just <$> genAssetAmount
        Income         -> Just <$> genAssetAmount
        FundingPayment -> Just <$> genAssetAmount
        TransferIn     -> Just <$> genAssetAmount
        PerpOpen       -> Just <$> genAssetAmount
        _ -> pure Nothing
      closedPnl <- case txType' of
        PerpClose -> Just . T.pack . show <$> (choose (-5000, 5000) :: Gen Integer)
        _         -> pure Nothing
      pure $ Transaction txId' ts "test" "ethereum" txType' "0xabc" Nothing sent rcv Nothing Nothing Nothing Nothing Nothing closedPnl

    genAssetAmount = do
      asset <- elements ["ETH", "BTC", "SOL", "USDC"]
      canonical <- elements [Nothing, Just "mint123"]
      amt <- show <$> (choose (1, 99999) :: Gen Integer)
      usd <- show <$> (choose (1, 99999) :: Gen Integer)
      pure $ AssetAmount (T.pack asset) canonical (T.pack amt) (T.pack usd)

-- ---------------------------------------------------------------------------
-- 12. Out-of-order timestamps are handled (sorted internally)
-- ---------------------------------------------------------------------------

prop_outOfOrderHandled :: Property
prop_outOfOrderHandled = forAll genAmtPx $ \(amt, px) ->
  let t1 = UTCTime (fromGregorian 2024 1 1) 0
      t2 = UTCTime (fromGregorian 2024 6 1) 0
      buyTx = Transaction
        { txId = "buy1", txTimestamp = t1, txSource = "test", txChain = "test"
        , txType = Buy, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "ETH" Nothing (showI amt) (showI (amt * px)))
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      sellTx = Transaction
        { txId = "sell1", txTimestamp = t2, txSource = "test", txChain = "test"
        , txType = Sell, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" Nothing (showI amt) (showI (amt * px)))
        , txReceived = Nothing, txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      -- Reverse order: sell before buy
      result = processTransactions [sellTx, buyTx]
  in property (null (prErrors result))
  where
    genAmtPx = (,) <$> choose (1, 100 :: Integer) <*> choose (1, 5000 :: Integer)

-- ---------------------------------------------------------------------------
-- 13. Multi-lot disposal: 3+ lots → 3+ gain records, proceeds sum correctly
-- ---------------------------------------------------------------------------

prop_multiLotDisposal :: Property
prop_multiLotDisposal = forAll genMultiLot $ \(amts, prices, sellPx) ->
  let n = length amts
      times = [UTCTime (fromGregorian 2024 m 1) 0 | m <- [1..n]]
      sellTime = UTCTime (fromGregorian 2025 1 1) 0
      totalAmt = sum amts
      queue = foldl (\q (amt, px, t) ->
        Lot.acquire "ETH" t (TokenAmount amt) (USD (amt * px)) q) Lot.empty
        (zip3 amts prices times)
      proceeds = USD (totalAmt * sellPx)
      disp = Disposal "ETH" sellTime (TokenAmount totalAmt) proceeds 0
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (gains, _) ->
         conjoin
           [ property (length gains >= 3)
           , sum (map glAmount gains) === TokenAmount totalAmt
           , sum (map glProceeds gains) === proceeds
           ]
  where
    genMultiLot = do
      k <- choose (3, 6)
      amts   <- vectorOf k genPositiveAmount
      prices <- vectorOf k genPrice
      sellPx <- genPrice
      pure (amts, prices, sellPx)

-- ---------------------------------------------------------------------------
-- 14. Dust precision: 18-decimal amounts survive the pipeline
-- ---------------------------------------------------------------------------

prop_dustPrecision :: Property
prop_dustPrecision = once $
  let dust = 1 % (10 ^ (18 :: Int))
      t1 = UTCTime (fromGregorian 2024 1 1) 0
      t2 = UTCTime (fromGregorian 2024 6 1) 0
      px  = 3000 :: Rational
      queue = Lot.acquire "ETH" t1 (TokenAmount dust) (USD (dust * px)) Lot.empty
      disp  = Disposal "ETH" t2 (TokenAmount dust) (USD (dust * px * 2)) 0
    in case Lot.dispose disp queue of
         Left _  -> property False
         Right (gains, queue') -> case gains of
          [gain] -> conjoin
            [ Lot.totalUnits "ETH" queue' === 0
            , glAmount gain === TokenAmount dust
            ]
          _ -> counterexample ("expected 1 gain, got " ++ show (length gains)) False

-- ---------------------------------------------------------------------------
-- 15. Zero-value transactions don't corrupt the queue
-- ---------------------------------------------------------------------------

prop_zeroValueSafe :: Property
prop_zeroValueSafe = forAll genPositiveAmount $ \amt ->
  forAll genPrice $ \px ->
    let t = UTCTime (fromGregorian 2024 1 1) 0
        queue0 = Lot.acquire "ETH" t (TokenAmount amt) (USD (amt * px)) Lot.empty
        queue1 = Lot.acquire "ETH" t 0 0 queue0
    in conjoin
          [ Lot.totalUnits "ETH" queue1 === TokenAmount amt
          , Lot.totalCostBasis queue1 === USD (amt * px)
          ]

prop_zeroAcquisitionDoesNotEmitGain :: Property
prop_zeroAcquisitionDoesNotEmitGain = once $
  let t0 = UTCTime (fromGregorian 2024 1 1) 0
      t1 = UTCTime (fromGregorian 2024 6 1) 0
      queue = Lot.acquire "ETH" t0 0 0 $
              Lot.acquire "ETH" t0 1 1000 Lot.empty
      disp = Disposal "ETH" t1 1 2000 0
  in case Lot.dispose disp queue of
       Left err -> counterexample (T.unpack err) False
       Right (gains, queue') -> conjoin
         [ property (length gains == 1)
         , property (all ((/= 0) . glAmount) gains)
         , property (Map.notMember "ETH" queue')
         ]

prop_insufficientLotErrorPadsSmallDecimals :: Property
prop_insufficientLotErrorPadsSmallDecimals = once $
  let t = UTCTime (fromGregorian 2024 1 1) 0
      disp = Disposal "ETH" t (TokenAmount (1 % 1000000)) 0 0
  in case Lot.dispose disp Lot.empty of
       Right _ -> property False
       Left err -> property ("0.000001" `T.isInfixOf` err)

-- ---------------------------------------------------------------------------
-- 16. Positive funding creates ordinary income and a USDC lot
-- ---------------------------------------------------------------------------

prop_positiveFundingCreatesIncomeAndLot :: Property
prop_positiveFundingCreatesIncomeAndLot = once $
  let t = UTCTime (fromGregorian 2025 12 2) 0
      tx = Transaction
        { txId = "funding-positive", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = FundingPayment, txWallet = "w1", txCounterparty = Nothing
        , txSent = Nothing
        , txReceived = Just (AssetAmount "USDC" Nothing "1.879512" "1.879512")
        , txFee = Nothing, txRawType = Just "funding", txMarket = Just "ETH", txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [tx]
  in case prIncome result of
       [income] -> conjoin
         [ property (null (prErrors result))
         , iiUSDValue income === USD (469878 % 250000)
         , iiAsset income === "USDC"
         , iiMarket income === Just "ETH"
         , Lot.totalUnits "USDC" (prFinalQueue result) === TokenAmount (469878 % 250000)
         , Lot.totalCostBasis (prFinalQueue result) === USD (469878 % 250000)
         ]
       entries -> counterexample ("expected 1 income entry, got " ++ show (length entries)) False

-- ---------------------------------------------------------------------------
-- 17. Negative funding produces a structured FundingExpense (not an error)
-- ---------------------------------------------------------------------------

prop_negativeFundingCreatesStructuredExpense :: Property
prop_negativeFundingCreatesStructuredExpense = once $
  let t = UTCTime (fromGregorian 2025 10 7) 0
      tx = Transaction
        { txId = "funding-negative", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = FundingPayment, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "USDC" Nothing "0.168095" "0.168095")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Just "funding", txMarket = Just "BTC", txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [tx]
  in case prFundingExpenses result of
       [expense] -> conjoin
         [ property (null (prErrors result))
         , property (null (prIncome result))
         , Lot.totalUnits "USDC" (prFinalQueue result) === 0
         , feUSDValue expense === USD (168095 % 1000000)
         , feAsset expense === "USDC"
         , feMarket expense === Just "BTC"
         ]
       entries -> counterexample ("expected 1 funding expense, got " ++ show (length entries)) False

-- ---------------------------------------------------------------------------
-- 18. Perp open does not create lots (quarantined from spot FIFO)
-- ---------------------------------------------------------------------------

prop_perpOpenDoesNotCreateLots :: Property
prop_perpOpenDoesNotCreateLots = once $
  let t = UTCTime (fromGregorian 2025 1 15) 0
      tx = Transaction
        { txId = "perp-open-1", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpOpen, txWallet = "w1", txCounterparty = Nothing
        , txSent = Nothing
        , txReceived = Just (AssetAmount "BTC" Nothing "0.0004" "49.93")
        , txFee = Nothing, txRawType = Just "Open Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [tx]
  in conjoin
       [ property (null (prErrors result))
       , property (null (prGainLosses result))
       , property (null (prPerpPnl result))
       , Lot.totalUnits "BTC" (prFinalQueue result) === 0
       ]

-- ---------------------------------------------------------------------------
-- 19. Perp close uses ClosedPnl for realized PnL output
-- ---------------------------------------------------------------------------

prop_perpCloseUsesClosedPnl :: Property
prop_perpCloseUsesClosedPnl = once $
  let t = UTCTime (fromGregorian 2025 2 1) 0
      tx = Transaction
        { txId = "perp-close-1", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpClose, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "SOL" Nothing "10.5" "2100.00")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Just "Close Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Just "42.50"
        }
      result = processTransactions [tx]
  in case prPerpPnl result of
       [pnl] -> conjoin
         [ property (null (prErrors result))
         , property (null (prGainLosses result))
         , ppClosedPnl pnl === USD (85 % 2)
         , ppAsset pnl === "SOL"
         , ppDirection pnl === "Close Long"
         -- Perp close does NOT consume spot lots
         , Lot.totalUnits "SOL" (prFinalQueue result) === 0
         ]
       entries -> counterexample ("expected 1 perp PnL entry, got " ++ show (length entries)) False

-- ---------------------------------------------------------------------------
-- 20. Perp close without ClosedPnl surfaces error
-- ---------------------------------------------------------------------------

prop_perpCloseWithoutPnlIsError :: Property
prop_perpCloseWithoutPnlIsError = once $
  let t = UTCTime (fromGregorian 2025 2 1) 0
      tx = Transaction
        { txId = "perp-close-no-pnl", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpClose, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "SOL" Nothing "10.5" "2100.00")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Just "Close Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [tx]
  in conjoin
       [ property (length (prErrors result) == 1)
       , property (null (prPerpPnl result))
       ]

-- ---------------------------------------------------------------------------
-- 21. Perp close without sent asset surfaces error
-- ---------------------------------------------------------------------------

prop_perpCloseWithoutSentIsError :: Property
prop_perpCloseWithoutSentIsError = once $
  let t = UTCTime (fromGregorian 2025 2 1) 0
      tx = Transaction
        { txId = "perp-close-no-sent", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpClose, txWallet = "w1", txCounterparty = Nothing
        , txSent = Nothing, txReceived = Nothing
        , txFee = Nothing, txRawType = Just "Close Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Just "42.50"
        }
      result = processTransactions [tx]
  in conjoin
       [ property (length (prErrors result) == 1)
       , property (null (prPerpPnl result))
       ]

-- ---------------------------------------------------------------------------
-- 22. Perp fills do not contaminate spot FIFO queues
-- ---------------------------------------------------------------------------

prop_perpDoesNotContaminateSpotFIFO :: Property
prop_perpDoesNotContaminateSpotFIFO = once $
  let t1 = UTCTime (fromGregorian 2025 1 1) 0
      t2 = UTCTime (fromGregorian 2025 1 15) 0
      t3 = UTCTime (fromGregorian 2025 2 1) 0
      t4 = UTCTime (fromGregorian 2025 3 1) 0
      -- Spot buy: acquire 1 BTC at $40000
      spotBuy = Transaction
        { txId = "spot-buy", txTimestamp = t1, txSource = "robinhood", txChain = "robinhood"
        , txType = Buy, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "BTC" Nothing "1" "40000")
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      -- Perp open: should NOT create a phantom BTC lot
      perpOpen = Transaction
        { txId = "perp-open", txTimestamp = t2, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpOpen, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "BTC" Nothing "5" "250000")
        , txFee = Nothing, txRawType = Just "Open Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      -- Perp close: should NOT consume the spot BTC lot
      perpClose = Transaction
        { txId = "perp-close", txTimestamp = t3, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = PerpClose, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "BTC" Nothing "5" "260000")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Just "Close Long", txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Just "10000"
        }
      -- Spot sell: should find the original spot lot intact
      spotSell = Transaction
        { txId = "spot-sell", txTimestamp = t4, txSource = "robinhood", txChain = "robinhood"
        , txType = Sell, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "BTC" Nothing "1" "50000")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [spotBuy, perpOpen, perpClose, spotSell]
  in case (prGainLosses result, prPerpPnl result) of
       ([gain], [pnl]) -> conjoin
         [ -- Spot sell should succeed (lot still exists)
           property (null (prErrors result))
         , glGain gain === USD 10000
         , ppClosedPnl pnl === USD 10000
         ]
       (gains, pnls) -> counterexample
         ("expected 1 gain and 1 perp PnL entry, got " ++ show (length gains) ++ " gains and " ++ show (length pnls) ++ " PnL entries")
         False

-- ---------------------------------------------------------------------------
-- 23. CSV rendering escapes source-backed text fields
-- ---------------------------------------------------------------------------

prop_csvEscapesSourceTextFields :: Property
prop_csvEscapesSourceTextFields = once $
  let t = UTCTime (fromGregorian 2024 1 1) 0
      incomeCsv = renderIncomeCSV
        [ IncomeEntry
            { iiTimestamp = t
            , iiTxId      = "tx,\"1"
            , iiAsset     = "ABC,DEF"
            , iiAmount    = 1
            , iiUSDValue  = 2
            , iiMarket    = Just "BTC,PERP"
            }
        ]
      perpCsv = renderPerpPnlCSV
        [ PerpPnlEntry
            { ppTimestamp = t
            , ppTxId      = "tx-2"
            , ppAsset     = "SOL"
            , ppAmount    = 1
            , ppDirection = "Close, Long"
            , ppClosedPnl = 3
            }
        ]
  in conjoin
       [ property ("Date,Tx ID,Asset,Amount,USD Value,Market" `T.isInfixOf` incomeCsv)
       , property ("\"tx,\"\"1\",\"ABC,DEF\"" `T.isInfixOf` incomeCsv)
       , property ("\"BTC,PERP\"" `T.isInfixOf` incomeCsv)
       , property ("\"Close, Long\"" `T.isInfixOf` perpCsv)
       ]

-- ---------------------------------------------------------------------------
-- 24. parseDecimal accepts exactly the canonical decimal grammar
-- ---------------------------------------------------------------------------

prop_parseDecimalAcceptsCanonical :: Property
prop_parseDecimalAcceptsCanonical = once $ conjoin
  [ parseDecimal "0"                    === 0
  , parseDecimal "-0.5"                 === ((-1) % 2)
  , parseDecimal "1.234567890123456789" === (1234567890123456789 % (10 ^ (18 :: Integer)))
  , parseDecimal "42"                   === 42
  ]

prop_parseDecimalRejectsMalformed :: Property
prop_parseDecimalRejectsMalformed = once $ ioProperty $ do
  let malformed = ["", "1e5", "+5", "abc", "1.2.3", "1.", ".5", " 1"] :: [Text]
  results <- mapM rejects malformed
  pure $ conjoin
    [ counterexample ("expected parseDecimal to reject " ++ show t) ok
    | (t, ok) <- zip malformed results
    ]
  where
    rejects t = do
      -- fromRational forces the full Rational; parseDecimal errors are lazy.
      outcome <- try (evaluate (fromRational (parseDecimal t) :: Double))
      pure $ case outcome of
        Left (ErrorCall _) -> True
        Right _            -> False

-- ---------------------------------------------------------------------------
-- 25. Negative-amount rows are surfaced as errors, never folded into FIFO
-- ---------------------------------------------------------------------------

prop_negativeRowsSurfacedNotApplied :: Property
prop_negativeRowsSurfacedNotApplied = once $
  let t1 = UTCTime (fromGregorian 2024 1 1) 0
      t2 = UTCTime (fromGregorian 2024 2 1) 0
      t3 = UTCTime (fromGregorian 2024 3 1) 0
      goodBuy = Transaction
        { txId = "good-buy", txTimestamp = t1, txSource = "test", txChain = "test"
        , txType = Buy, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "ETH" Nothing "2" "4000")
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      badBuy = goodBuy
        { txId = "bad-buy", txTimestamp = t2
        , txReceived = Just (AssetAmount "ETH" Nothing "-1" "2000")
        }
      badSell = Transaction
        { txId = "bad-sell", txTimestamp = t3, txSource = "test", txChain = "test"
        , txType = Sell, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" Nothing "-1" "3000")
        , txReceived = Nothing, txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [goodBuy, badBuy, badSell]
  in conjoin
       [ property (length (prErrors result) == 2)
       , property (null (prGainLosses result))
       -- The good buy is intact; the bad rows changed nothing.
       , Lot.totalUnits "ETH" (prFinalQueue result) === TokenAmount 2
       , Lot.totalCostBasis (prFinalQueue result) === USD 4000
       ]

prop_zeroAmountNonzeroBasisSurfaced :: Property
prop_zeroAmountNonzeroBasisSurfaced = once $
  let t = UTCTime (fromGregorian 2024 1 1) 0
      tx = Transaction
        { txId = "zero-amt-buy", txTimestamp = t, txSource = "test", txChain = "test"
        , txType = Buy, txWallet = "w1", txCounterparty = Nothing, txSent = Nothing
        , txReceived = Just (AssetAmount "ETH" Nothing "0" "150.25")
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [tx]
  in conjoin
       [ property (length (prErrors result) == 1)
       , Lot.totalCostBasis (prFinalQueue result) === USD 0
       ]

-- ---------------------------------------------------------------------------
-- 26. Swap with failed disposal still acquires the received leg
-- ---------------------------------------------------------------------------

prop_swapFailedDisposalStillAcquires :: Property
prop_swapFailedDisposalStillAcquires = once $
  let t = UTCTime (fromGregorian 2024 6 1) 0
      -- No prior ETH lots exist, so the sent-leg disposal must fail.
      swapTx = Transaction
        { txId = "swap-no-lots", txTimestamp = t, txSource = "test", txChain = "test"
        , txType = Swap, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" Nothing "1" "3000")
        , txReceived = Just (AssetAmount "SOL" Nothing "20" "3000")
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [swapTx]
  in conjoin
       [ property (length (prErrors result) == 1)
       , property (null (prGainLosses result))
       -- The received leg is explicit in the IR and must not be dropped.
       , Lot.totalUnits "SOL" (prFinalQueue result) === TokenAmount 20
       , Lot.totalCostBasis (prFinalQueue result) === USD 3000
       ]

-- ---------------------------------------------------------------------------
-- 27. Transfer rows are preserved in prTransfers, not silently dropped
-- ---------------------------------------------------------------------------

prop_transfersAreRecorded :: Property
prop_transfersAreRecorded = once $
  let t1 = UTCTime (fromGregorian 2024 3 1) 0
      t2 = UTCTime (fromGregorian 2024 3 2) 0
      outTx = Transaction
        { txId = "xfer-out", txTimestamp = t1, txSource = "etherscan", txChain = "ethereum"
        , txType = TransferOut, txWallet = "wallet-a", txCounterparty = Just "wallet-b"
        , txSent = Just (AssetAmount "ETH" Nothing "0.5" "1050")
        , txReceived = Nothing, txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      inTx = Transaction
        { txId = "xfer-in", txTimestamp = t2, txSource = "etherscan", txChain = "ethereum"
        , txType = TransferIn, txWallet = "wallet-b", txCounterparty = Just "wallet-a"
        , txSent = Nothing
        , txReceived = Just (AssetAmount "ETH" Nothing "0.5" "1050")
        , txFee = Nothing, txRawType = Nothing, txMarket = Nothing, txEventGroupId = Nothing, txSplitReason = Nothing, txClosedPnl = Nothing
        }
      result = processTransactions [outTx, inTx]
  in case prTransfers result of
       [a, b] -> conjoin
         [ property (null (prErrors result))
         , property (null (prGainLosses result))
         , teDirection a === DirOut
         , teTxId a === "xfer-out"
         , teAsset a === "ETH"
         , teAmount a === TokenAmount (1 % 2)
         , teWallet a === "wallet-a"
         , teCounterparty a === Just "wallet-b"
         , teDirection b === DirIn
         , teWallet b === "wallet-b"
         -- Transfers never touch FIFO lots.
         , Lot.totalUnits "ETH" (prFinalQueue result) === 0
         ]
       entries -> counterexample ("expected 2 transfer entries, got " ++ show (length entries)) False

-- ---------------------------------------------------------------------------
-- 28. Fee allocation across multiple lots is exact
-- ---------------------------------------------------------------------------

prop_feeAllocationExact :: Property
prop_feeAllocationExact = forAll genMultiLotWithFee $ \(amts, prices, sellPx, feeR) ->
  let n = length amts
      times = [UTCTime (fromGregorian 2024 m 1) 0 | m <- [1..n]]
      sellTime = UTCTime (fromGregorian 2025 1 1) 0
      totalAmt = sum amts
      totalBasis = sum [amt * px | (amt, px) <- zip amts prices]
      queue = foldl (\q (amt, px, t) ->
        Lot.acquire "ETH" t (TokenAmount amt) (USD (amt * px)) q) Lot.empty
        (zip3 amts prices times)
      proceeds = totalAmt * sellPx
      disp = Disposal "ETH" sellTime (TokenAmount totalAmt) (USD proceeds) (USD feeR)
  in case Lot.dispose disp queue of
       Left _  -> property False
       Right (gains, _) ->
         cover 50 (length gains >= 3) "multi-lot" $
         conjoin
           [ sum (map glGain gains)     === USD (proceeds - feeR - totalBasis)
           , sum (map glProceeds gains) === USD (proceeds - feeR)
           ]
  where
    genMultiLotWithFee = do
      k <- choose (3, 6)
      amts   <- vectorOf k genPositiveAmount
      prices <- vectorOf k genPrice
      sellPx <- genPrice
      feeN   <- choose (1, 999 :: Integer)
      feeD   <- choose (1, 100 :: Integer)
      pure (amts, prices, sellPx, feeN % feeD)

-- ---------------------------------------------------------------------------
-- 29. Spreadsheet formula injection guard (shared csvField path, all CSVs)
-- ---------------------------------------------------------------------------

prop_csvFormulaInjectionGuard :: Property
prop_csvFormulaInjectionGuard = once $
  let t = UTCTime (fromGregorian 2024 1 1) 0
      incomeCsv = renderIncomeCSV
        [ IncomeEntry
            { iiTimestamp = t
            , iiTxId      = "=SUM(A1:A9)"
            , iiAsset     = "ETH"
            , iiAmount    = 1
            , iiUSDValue  = 2
            , iiMarket    = Nothing
            }
        ]
      negCsv = renderIncomeCSV
        [ IncomeEntry
            { iiTimestamp = t
            , iiTxId      = "tx-neg"
            , iiAsset     = "ETH"
            , iiAmount    = -1
            , iiUSDValue  = 2
            , iiMarket    = Nothing
            }
        ]
      transferCsv = renderTransfersCSV
        [ TransferEntry
            { teTimestamp    = t
            , teTxId         = "@cmd"
            , teDirection    = DirIn
            , teAsset        = "ETH"
            , teAmount       = 1
            , teUSDValue     = 2
            , teWallet       = "+wallet"
            , teCounterparty = Just "=2+5"
            }
        ]
  in conjoin
       [ property ("'=SUM(A1:A9)" `T.isInfixOf` incomeCsv)
       , property ("'@cmd"        `T.isInfixOf` transferCsv)
       , property ("'+wallet"     `T.isInfixOf` transferCsv)
       , property ("'=2+5"        `T.isInfixOf` transferCsv)
       -- '-' must NOT be guarded: negative amounts stay clean.
       , property (",-1," `T.isInfixOf` negCsv)
       , property (not ("'-" `T.isInfixOf` negCsv))
       ]

-- ---------------------------------------------------------------------------
-- 30. Golden fixture: buy + sell + own-wallet transfer => exact 8949 CSV
-- ---------------------------------------------------------------------------

golden_basicBuySellTransfer :: IO ()
golden_basicBuySellTransfer = do
  inputPath <- getDataFileName "testdata/basic-buy-sell-transfer.json"
  expectedPath <- getDataFileName "testdata/basic-buy-sell-transfer-8949.csv"

  input <- BL.readFile inputPath
  expected <- TIO.readFile expectedPath

  payload <- case Aeson.eitherDecode input of
    Left err     -> failSpec $ "failed to decode golden fixture: " ++ err
    Right parsed -> pure parsed

  let result = processTransactions (payloadTransactions payload)
  unless (null (prErrors result)) $
    failSpec $ "golden fixture produced processing errors: " ++ show (prErrors result)

  let actual = render8949CSV (prGainLosses result)
  unless (actual == expected) $
    failSpec $ unlines
      [ "golden fixture CSV mismatch"
      , "expected:"
      , show expected
      , "actual:"
      , show actual
      ]

failSpec :: String -> IO a
failSpec message = do
  putStrLn message
  exitFailure

-- ---------------------------------------------------------------------------
-- Main
-- ---------------------------------------------------------------------------

main :: IO ()
main = do
  putStrLn "Running QuickCheck properties...\n"

  let check name prop = do
        putStr $ "  " ++ name ++ ": "
        result <- quickCheckWithResult stdArgs{maxSuccess=200} prop
        unless (isSuccess result) exitFailure

  check "1.  conservation of units"     prop_conservationOfUnits
  check "2.  conservation of cost basis" prop_conservationOfCostBasis
  check "3.  non-negative lots"          prop_nonNegativeLots
  check "4.  FIFO ordering"              prop_fifoOrdering
  check "5.  zero disposal is no-op"     prop_zeroDisposalNoop
  check "6.  gain formula exact"         prop_gainFormula
  check "7.  holding period calendar"    prop_holdingPeriodCalendarRule
  check "8.  holding period anniversary" prop_holdingPeriodAnniversaryCases
  check "9.  no phantom gains"           prop_noPhantomGains
  check "10. buy-then-sell roundtrip"    prop_buyThenSellExact
  check "11. swap decomposition"         prop_swapDecomposition
  check "12. JSON roundtrip"             prop_jsonRoundtrip
  check "13. out-of-order handled"       prop_outOfOrderHandled
  check "14. multi-lot disposal"         prop_multiLotDisposal
  check "15. dust precision"             prop_dustPrecision
  check "16. zero-value safety"          prop_zeroValueSafe
  check "17. zero acquisition no gain"   prop_zeroAcquisitionDoesNotEmitGain
  check "18. small decimal errors"       prop_insufficientLotErrorPadsSmallDecimals
  check "19. positive funding income"    prop_positiveFundingCreatesIncomeAndLot
  check "20. negative funding expense"   prop_negativeFundingCreatesStructuredExpense
  check "21. perp open no lots"          prop_perpOpenDoesNotCreateLots
  check "22. perp close ClosedPnl"       prop_perpCloseUsesClosedPnl
  check "23. perp close without pnl"     prop_perpCloseWithoutPnlIsError
  check "24. perp close requires sent"   prop_perpCloseWithoutSentIsError
  check "25. perp/spot isolation"        prop_perpDoesNotContaminateSpotFIFO
  check "26. CSV escaping"               prop_csvEscapesSourceTextFields
  check "27. parseDecimal canonical"     prop_parseDecimalAcceptsCanonical
  check "28. parseDecimal rejects"       prop_parseDecimalRejectsMalformed
  check "29. negative rows surfaced"     prop_negativeRowsSurfacedNotApplied
  check "30. zero-amt basis surfaced"    prop_zeroAmountNonzeroBasisSurfaced
  check "31. swap failed disposal"       prop_swapFailedDisposalStillAcquires
  check "32. transfers recorded"         prop_transfersAreRecorded
  check "33. fee allocation exact"       prop_feeAllocationExact
  check "34. CSV formula injection"      prop_csvFormulaInjectionGuard

  putStr "  35. golden buy/sell/transfer: "
  golden_basicBuySellTransfer
  putStrLn "OK"

  putStrLn "\nAll properties passed."
