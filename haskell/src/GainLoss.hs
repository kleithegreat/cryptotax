{-# LANGUAGE OverloadedStrings #-}

module GainLoss
  ( processTransactions
  , ProcessResult(..)
  ) where

import           Data.List (sortOn)
import           Data.Text (Text)
import           Types
import           Lot       (LotQueue)
import qualified Lot

data ProcessResult = ProcessResult
  { prGainLosses      :: [GainLoss]
  , prIncome          :: [IncomeEntry]
  , prFundingExpenses :: [FundingExpense]
  , prPerpPnl         :: [PerpPnlEntry]
  , prFinalQueue      :: LotQueue
  , prErrors          :: [Text]
  } deriving (Show)

-- | Accumulator threaded through the transaction fold.
data AccState = AccState
  { stQueue           :: !LotQueue
  , stGains           :: [GainLoss]
  , stIncome          :: [IncomeEntry]
  , stFundingExpenses :: [FundingExpense]
  , stPerpPnl         :: [PerpPnlEntry]
  , stErrors          :: [Text]
  }

initialState :: AccState
initialState = AccState Lot.empty [] [] [] [] []

-- | Process transactions through the FIFO lot engine.
-- Input is sorted by timestamp before processing.
processTransactions :: [Transaction] -> ProcessResult
processTransactions txs =
  let sorted = sortOn txTimestamp txs
      AccState queue gains income fundExp perpPnl errs = foldl processTx initialState sorted
  in ProcessResult
       { prGainLosses      = reverse gains
       , prIncome          = reverse income
       , prFundingExpenses = reverse fundExp
       , prPerpPnl         = reverse perpPnl
       , prFinalQueue      = queue
       , prErrors          = reverse errs
       }

processTx :: AccState -> Transaction -> AccState
processTx st tx = case txType tx of
  Buy            -> handleBuy st tx
  Sell           -> handleSell st tx
  Swap           -> handleSwap st tx
  TransferIn     -> st
  TransferOut    -> st
  Income         -> handleIncome st tx
  FundingPayment -> handleFunding st tx
  PerpOpen       -> handlePerpOpen st tx
  PerpClose      -> handlePerpClose st tx

handleBuy :: AccState -> Transaction -> AccState
handleBuy st tx = case txReceived tx of
  Nothing -> st { stErrors = "Buy without received asset" : stErrors st }
  Just rcv ->
    let amt   = TokenAmount (parseDecimal (aaAmount rcv))
        cost  = USD (parseDecimal (aaUSDValue rcv)) + feeUSD tx
        queue = Lot.acquire (AssetSymbol (aaAsset rcv)) (txTimestamp tx) amt cost (stQueue st)
    in st { stQueue = queue }

handleSell :: AccState -> Transaction -> AccState
handleSell st tx = case txSent tx of
  Nothing -> st { stErrors = "Sell without sent asset" : stErrors st }
  Just snt ->
    let disp = Disposal
          { dispAsset    = AssetSymbol (aaAsset snt)
          , dispTime     = txTimestamp tx
          , dispAmount   = TokenAmount (parseDecimal (aaAmount snt))
          , dispProceeds = USD (parseDecimal (aaUSDValue snt))
          , dispFee      = feeUSD tx
          }
    in case Lot.dispose disp (stQueue st) of
         Left err -> st { stErrors = err : stErrors st }
         Right (newGains, queue) ->
           st { stQueue = queue, stGains = newGains ++ stGains st }

handleSwap :: AccState -> Transaction -> AccState
handleSwap st tx = case (txSent tx, txReceived tx) of
  (Just snt, Just rcv) ->
    let disp = Disposal
          { dispAsset    = AssetSymbol (aaAsset snt)
          , dispTime     = txTimestamp tx
          , dispAmount   = TokenAmount (parseDecimal (aaAmount snt))
          , dispProceeds = USD (parseDecimal (aaUSDValue snt))
          , dispFee      = feeUSD tx
          }
    in case Lot.dispose disp (stQueue st) of
         Left err -> st { stErrors = err : stErrors st }
         Right (newGains, queue) ->
           let amt   = TokenAmount (parseDecimal (aaAmount rcv))
               cost  = USD (parseDecimal (aaUSDValue rcv))
               queue' = Lot.acquire (AssetSymbol (aaAsset rcv)) (txTimestamp tx) amt cost queue
           in st { stQueue = queue', stGains = newGains ++ stGains st }
  _ -> st { stErrors = "Swap without both sent and received" : stErrors st }

handleIncome :: AccState -> Transaction -> AccState
handleIncome st tx = case txReceived tx of
  Nothing -> st { stErrors = "Income without received asset" : stErrors st }
  Just rcv -> recordIncomeReceipt st tx rcv

handleFunding :: AccState -> Transaction -> AccState
handleFunding st tx = case (txSent tx, txReceived tx) of
  (Nothing, Just rcv) -> recordIncomeReceipt st tx rcv
  (Just snt, Nothing) ->
    let amt = TokenAmount (parseDecimal (aaAmount snt))
        usd = USD (parseDecimal (aaUSDValue snt))
        expense = FundingExpense
          { feTimestamp = txTimestamp tx
          , feTxId      = txId tx
          , feAsset     = AssetSymbol (aaAsset snt)
          , feAmount    = amt
          , feUSDValue  = usd
          }
    in st { stFundingExpenses = expense : stFundingExpenses st }
  (Nothing, Nothing) ->
    st { stErrors = "Funding payment without sent or received asset" : stErrors st }
  (Just _, Just _) ->
    st { stErrors = "Funding payment with both sent and received assets" : stErrors st }

-- | Perp opens are quarantined from spot FIFO. No phantom lots are created.
-- The position is tracked by the exchange, not by this inventory engine.
handlePerpOpen :: AccState -> Transaction -> AccState
handlePerpOpen st _tx = st

-- | Perp closes use the exchange-reported ClosedPnl for realized PnL output.
-- No FIFO lots are consumed. The PnL entry is emitted to a separate report
-- because the 8949 cost-basis/proceeds representation for derivatives is unresolved.
handlePerpClose :: AccState -> Transaction -> AccState
handlePerpClose st tx = case (txClosedPnl tx, txSent tx) of
  (Nothing, _) ->
    st { stErrors = ("PerpClose without closed_pnl for tx " <> txId tx) : stErrors st }
  (_, Nothing) ->
    st { stErrors = ("PerpClose without sent asset for tx " <> txId tx) : stErrors st }
  (Just pnlText, Just snt) ->
    let pnl = USD (parseDecimal pnlText)
        asset = AssetSymbol (aaAsset snt)
        amount = TokenAmount (parseDecimal (aaAmount snt))
        direction = case txRawType tx of
          Just rt -> rt
          Nothing -> "unknown"
        entry = PerpPnlEntry
          { ppTimestamp = txTimestamp tx
          , ppTxId      = txId tx
          , ppAsset     = asset
          , ppAmount    = amount
          , ppDirection = direction
          , ppClosedPnl = pnl
          }
    in st { stPerpPnl = entry : stPerpPnl st }

feeUSD :: Transaction -> USD
feeUSD tx = case txFee tx of
  Nothing  -> 0
  Just fee -> USD (parseDecimal (aaUSDValue fee))

recordIncomeReceipt :: AccState -> Transaction -> AssetAmount -> AccState
recordIncomeReceipt st tx rcv =
  let amt   = TokenAmount (parseDecimal (aaAmount rcv))
      fmv   = USD (parseDecimal (aaUSDValue rcv))
      queue = Lot.acquire (AssetSymbol (aaAsset rcv)) (txTimestamp tx) amt fmv (stQueue st)
      entry = IncomeEntry
        { iiTimestamp = txTimestamp tx
        , iiTxId      = txId tx
        , iiAsset     = AssetSymbol (aaAsset rcv)
        , iiAmount    = amt
        , iiUSDValue  = fmv
        }
  in st { stQueue = queue, stIncome = entry : stIncome st }
