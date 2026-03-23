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
  { prGainLosses :: [GainLoss]
  , prIncome     :: [GainLoss]
  , prFinalQueue :: LotQueue
  , prErrors     :: [Text]
  } deriving (Show)

-- | Accumulator threaded through the transaction fold.
data AccState = AccState
  { stQueue  :: !LotQueue
  , stGains  :: [GainLoss]
  , stIncome :: [GainLoss]
  , stErrors :: [Text]
  }

initialState :: AccState
initialState = AccState Lot.empty [] [] []

-- | Process transactions through the FIFO lot engine.
-- Input is sorted by timestamp before processing.
processTransactions :: [Transaction] -> ProcessResult
processTransactions txs =
  let sorted = sortOn txTimestamp txs
      AccState queue gains income errs = foldl processTx initialState sorted
  in ProcessResult
       { prGainLosses = reverse gains
       , prIncome     = reverse income
       , prFinalQueue = queue
       , prErrors     = reverse errs
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
  Just rcv ->
    let amt   = TokenAmount (parseDecimal (aaAmount rcv))
        fmv   = USD (parseDecimal (aaUSDValue rcv))
        queue = Lot.acquire (AssetSymbol (aaAsset rcv)) (txTimestamp tx) amt fmv (stQueue st)
        entry = GainLoss
          { glAsset     = AssetSymbol (aaAsset rcv)
          , glAcquired  = txTimestamp tx
          , glDisposed  = txTimestamp tx
          , glAmount    = amt
          , glCostBasis = 0
          , glProceeds  = fmv
          , glGain      = fmv
          , glPeriod    = ShortTerm
          }
    in st { stQueue = queue, stIncome = entry : stIncome st }

handleFunding :: AccState -> Transaction -> AccState
handleFunding st tx = case txReceived tx of
  Nothing -> st  -- negative funding = expense, skip for now
  Just rcv ->
    let fmv   = USD (parseDecimal (aaUSDValue rcv))
        entry = GainLoss
          { glAsset     = AssetSymbol (aaAsset rcv)
          , glAcquired  = txTimestamp tx
          , glDisposed  = txTimestamp tx
          , glAmount    = TokenAmount (parseDecimal (aaAmount rcv))
          , glCostBasis = 0
          , glProceeds  = fmv
          , glGain      = fmv
          , glPeriod    = ShortTerm
          }
    in st { stIncome = entry : stIncome st }

feeUSD :: Transaction -> USD
feeUSD tx = case txFee tx of
  Nothing  -> 0
  Just fee -> USD (parseDecimal (aaUSDValue fee))
