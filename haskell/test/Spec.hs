{-# LANGUAGE OverloadedStrings    #-}
{-# LANGUAGE ScopedTypeVariables  #-}

module Main (main) where

import           Control.Monad      (unless)
import           Test.QuickCheck
import qualified Data.Aeson      as Aeson
import qualified Data.ByteString.Lazy as BL
import qualified Data.Map.Strict as Map
import           Data.Ratio      ((%))
import           Data.Text       (Text)
import qualified Data.Text       as T
import qualified Data.Text.IO    as TIO
import           Data.Time       (UTCTime(..), fromGregorian, secondsToDiffTime,
                                  nominalDay, addUTCTime)
import           Paths_cryptotax_core (getDataFileName)
import           System.Exit     (exitFailure)
import           Types
import           Lot             (LotQueue)
import qualified Lot
import           GainLoss        (processTransactions, ProcessResult(..))
import           Report          (render8949CSV)

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
  fraction <- choose (1, 100 :: Integer)
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
-- 7. Holding period correctness around the 365-day boundary
-- ---------------------------------------------------------------------------

prop_holdingPeriod :: Property
prop_holdingPeriod = forAll genPositiveAmount $ \amt ->
  forAll genPrice $ \px ->
    forAll (choose (1, 1000)) $ \(days :: Int) ->
      let buyTime  = UTCTime (fromGregorian 2022 1 1) 0
          sellTime = addUTCTime (fromIntegral days * nominalDay) buyTime
          queue    = Lot.acquire "ETH" buyTime (TokenAmount amt) (USD (amt * px)) Lot.empty
          disp     = Disposal "ETH" sellTime (TokenAmount amt) (USD (amt * px)) 0
          expected = if days > 365 then LongTerm else ShortTerm
      in cover 40 (days > 365) "long-term" $
         cover 40 (days <= 365) "short-term" $
         case Lot.dispose disp queue of
           Left _  -> property False
           Right (gains, _) ->
             property $ all (\g -> glPeriod g == expected) gains

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
        , txReceived = Just (AssetAmount "ETH" (showI sentAmt) (showI (sentAmt * sentPx)))
        , txFee = Nothing, txRawType = Nothing
        }
      swapTx = Transaction
        { txId = "swap1", txTimestamp = t2, txSource = "test", txChain = "test"
        , txType = Swap, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" (showI sentAmt) (showI (sentAmt * sentPx)))
        , txReceived = Just (AssetAmount "SOL" (showI rcvAmt) (showI (rcvAmt * rcvPx)))
        , txFee = Nothing, txRawType = Nothing
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
      txType' <- elements [Buy, Sell, Swap, TransferIn, TransferOut, Income, FundingPayment]
      sent <- case txType' of
        Sell -> Just <$> genAssetAmount
        Swap -> Just <$> genAssetAmount
        TransferOut -> Just <$> genAssetAmount
        _ -> pure Nothing
      rcv <- case txType' of
        Buy    -> Just <$> genAssetAmount
        Swap   -> Just <$> genAssetAmount
        Income -> Just <$> genAssetAmount
        FundingPayment -> Just <$> genAssetAmount
        TransferIn -> Just <$> genAssetAmount
        _ -> pure Nothing
      pure $ Transaction txId' ts "test" "ethereum" txType' "0xabc" Nothing sent rcv Nothing Nothing

    genAssetAmount = do
      asset <- elements ["ETH", "BTC", "SOL", "USDC"]
      amt <- show <$> (choose (1, 99999) :: Gen Integer)
      usd <- show <$> (choose (1, 99999) :: Gen Integer)
      pure $ AssetAmount (T.pack asset) (T.pack amt) (T.pack usd)

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
        , txReceived = Just (AssetAmount "ETH" (showI amt) (showI (amt * px)))
        , txFee = Nothing, txRawType = Nothing
        }
      sellTx = Transaction
        { txId = "sell1", txTimestamp = t2, txSource = "test", txChain = "test"
        , txType = Sell, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "ETH" (showI amt) (showI (amt * px)))
        , txReceived = Nothing, txFee = Nothing, txRawType = Nothing
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
       Right (gains, queue') ->
         conjoin
           [ Lot.totalUnits "ETH" queue' === 0
           , property (length gains == 1)
           , glAmount (head gains) === TokenAmount dust
           ]

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
        , txReceived = Just (AssetAmount "USDC" "1.879512" "1.879512")
        , txFee = Nothing, txRawType = Just "funding"
        }
      result = processTransactions [tx]
  in conjoin
       [ property (null (prErrors result))
       , property (length (prIncome result) == 1)
       , Lot.totalUnits "USDC" (prFinalQueue result) === TokenAmount (469878 % 250000)
       , Lot.totalCostBasis (prFinalQueue result) === USD (469878 % 250000)
       ]

-- ---------------------------------------------------------------------------
-- 17. Negative funding is preserved but surfaced as unsupported
-- ---------------------------------------------------------------------------

prop_negativeFundingIsExplicitlyUnsupported :: Property
prop_negativeFundingIsExplicitlyUnsupported = once $
  let t = UTCTime (fromGregorian 2025 10 7) 0
      tx = Transaction
        { txId = "funding-negative", txTimestamp = t, txSource = "hyperliquid", txChain = "hyperliquid"
        , txType = FundingPayment, txWallet = "w1", txCounterparty = Nothing
        , txSent = Just (AssetAmount "USDC" "0.168095" "0.168095")
        , txReceived = Nothing
        , txFee = Nothing, txRawType = Just "funding"
        }
      result = processTransactions [tx]
  in conjoin
       [ property (null (prIncome result))
       , Lot.totalUnits "USDC" (prFinalQueue result) === 0
       , prErrors result ===
           [ "Unsupported negative funding_payment expense for tx funding-negative: sent 0.168095 USDC; ordinary expense output and inventory adjustment are not implemented yet" ]
       ]

-- ---------------------------------------------------------------------------
-- 18. Golden fixture: buy + sell + own-wallet transfer => exact 8949 CSV
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
  check "7.  holding period"             prop_holdingPeriod
  check "8.  no phantom gains"           prop_noPhantomGains
  check "9.  buy-then-sell roundtrip"    prop_buyThenSellExact
  check "10. swap decomposition"         prop_swapDecomposition
  check "11. JSON roundtrip"             prop_jsonRoundtrip
  check "12. out-of-order handled"       prop_outOfOrderHandled
  check "13. multi-lot disposal"         prop_multiLotDisposal
  check "14. dust precision"             prop_dustPrecision
  check "15. zero-value safety"          prop_zeroValueSafe
  check "16. positive funding income"    prop_positiveFundingCreatesIncomeAndLot
  check "17. negative funding explicit"  prop_negativeFundingIsExplicitlyUnsupported

  putStr "  18. golden buy/sell/transfer: "
  golden_basicBuySellTransfer
  putStrLn "OK"

  putStrLn "\nAll properties passed."
