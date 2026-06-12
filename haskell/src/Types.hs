{-# LANGUAGE DeriveGeneric              #-}
{-# LANGUAGE DerivingStrategies         #-}
{-# LANGUAGE GeneralizedNewtypeDeriving #-}
{-# LANGUAGE LambdaCase                 #-}
{-# LANGUAGE OverloadedStrings          #-}
{-# LANGUAGE StrictData                 #-}

module Types
  ( TxPayload(..)
  , Transaction(..)
  , AssetAmount(..)
  , TxType(..)
  , TaxLot(..)
  , Disposal(..)
  , GainLoss(..)
  , HoldingPeriod(..)
  , AssetSymbol(..)
  , TokenAmount(..)
  , USD(..)
  , IncomeEntry(..)
  , FundingExpense(..)
  , PerpPnlEntry(..)
  , TransferDirection(..)
  , TransferEntry(..)
  , parseDecimal
  ) where

import           Data.Aeson
import           Data.Ratio     ((%))
import           Data.String    (IsString)
import           Data.Text      (Text)
import qualified Data.Text      as T
import           Data.Time      (UTCTime)
import           GHC.Generics   (Generic)

-- ---------------------------------------------------------------------------
-- Newtypes — prevent accidentally mixing USD with token amounts
-- ---------------------------------------------------------------------------

newtype AssetSymbol = AssetSymbol { unAsset :: Text }
  deriving stock   (Show, Eq, Ord, Generic)
  deriving newtype (IsString, FromJSON, ToJSON)

newtype TokenAmount = TokenAmount { unTokens :: Rational }
  deriving stock   (Show, Eq, Ord)
  deriving newtype (Num, Fractional, Real, RealFrac)

newtype USD = USD { unUSD :: Rational }
  deriving stock   (Show, Eq, Ord)
  deriving newtype (Num, Fractional, Real, RealFrac)

-- ---------------------------------------------------------------------------
-- Exact decimal parsing — no floating point ever touches financial values
-- ---------------------------------------------------------------------------

-- | Parse a canonical decimal string: optional '-', one or more digits,
-- optionally '.' followed by one or more digits. Anything else (empty string,
-- "+5", "1.", ".5", scientific notation, whitespace) is a Go→Haskell contract
-- violation: crash loudly instead of producing a silently wrong value.
parseDecimal :: Text -> Rational
parseDecimal t =
  case T.uncons t of
    Just ('-', rest) -> negate (parseUnsigned rest)
    _                -> parseUnsigned t
  where
    parseUnsigned :: Text -> Rational
    parseUnsigned s = case T.splitOn "." s of
      [whole] | allDigits whole -> fromInteger (readDigits whole)
      [whole, frac] | allDigits whole && allDigits frac ->
        fromInteger (readDigits whole) + readDigits frac % (10 ^ T.length frac)
      _ -> malformed

    allDigits :: Text -> Bool
    allDigits s = not (T.null s) && T.all (\c -> c >= '0' && c <= '9') s

    readDigits :: Text -> Integer
    readDigits = T.foldl' (\acc c -> acc * 10 + fromIntegral (fromEnum c - fromEnum '0')) 0

    malformed :: a
    malformed = error $
      "parseDecimal: malformed decimal (expected optional '-', digits, optional '.' and digits): "
      ++ show t

-- ---------------------------------------------------------------------------
-- JSON payload types (mirrors schema/transactions.json)
-- ---------------------------------------------------------------------------

data TxPayload = TxPayload
  { payloadVersion      :: Text
  , payloadWallets      :: [Text]
  , payloadTransactions :: [Transaction]
  } deriving (Show, Eq, Generic)

instance FromJSON TxPayload where
  parseJSON = withObject "TxPayload" $ \v ->
    TxPayload <$> v .: "version"
              <*> v .: "wallets"
              <*> v .: "transactions"

instance ToJSON TxPayload where
  toJSON p = object
    [ "version"      .= payloadVersion p
    , "wallets"      .= payloadWallets p
    , "transactions" .= payloadTransactions p
    ]

data TxType
  = Buy
  | Sell
  | Swap
  | TransferIn
  | TransferOut
  | Income
  | FundingPayment
  | PerpOpen
  | PerpClose
  deriving (Show, Eq, Generic)

instance FromJSON TxType where
  parseJSON = withText "TxType" $ \case
    "buy"             -> pure Buy
    "sell"            -> pure Sell
    "swap"            -> pure Swap
    "transfer_in"     -> pure TransferIn
    "transfer_out"    -> pure TransferOut
    "income"          -> pure Income
    "funding_payment" -> pure FundingPayment
    "perp_open"       -> pure PerpOpen
    "perp_close"      -> pure PerpClose
    other             -> fail $ "Unknown tx_type: " ++ T.unpack other

instance ToJSON TxType where
  toJSON ty = String $ case ty of
    Buy            -> "buy"
    Sell           -> "sell"
    Swap           -> "swap"
    TransferIn     -> "transfer_in"
    TransferOut    -> "transfer_out"
    Income         -> "income"
    FundingPayment -> "funding_payment"
    PerpOpen       -> "perp_open"
    PerpClose      -> "perp_close"

data AssetAmount = AssetAmount
  { aaAsset          :: Text
  , aaAssetCanonical :: Maybe Text  -- canonical identity (e.g. Solana mint) when it differs from display
  , aaAmount         :: Text        -- parsed to Rational in the core
  , aaUSDValue       :: Text        -- parsed to Rational in the core
  } deriving (Show, Eq, Generic)

instance FromJSON AssetAmount where
  parseJSON = withObject "AssetAmount" $ \v ->
    AssetAmount <$> v .:  "asset"
                <*> v .:? "asset_canonical"
                <*> v .:  "amount"
                <*> v .:  "usd_value"

instance ToJSON AssetAmount where
  toJSON a = object $
    [ "asset"     .= aaAsset a
    , "amount"    .= aaAmount a
    , "usd_value" .= aaUSDValue a
    ] ++ maybe [] (\c -> ["asset_canonical" .= c]) (aaAssetCanonical a)

data Transaction = Transaction
  { txId           :: Text
  , txTimestamp    :: UTCTime
  , txSource       :: Text
  , txChain        :: Text
  , txType         :: TxType
  , txWallet       :: Text
  , txCounterparty :: Maybe Text
  , txSent         :: Maybe AssetAmount
  , txReceived     :: Maybe AssetAmount
  , txFee          :: Maybe AssetAmount
  , txRawType      :: Maybe Text
  , txMarket       :: Maybe Text  -- source-specific market context, when preserved
  , txEventGroupId :: Maybe Text  -- stable key tying related rows to one source event
  , txSplitReason  :: Maybe Text  -- why one source event is represented as many rows
  , txClosedPnl    :: Maybe Text  -- exchange-reported realized PnL (perp closes)
  } deriving (Show, Eq, Generic)

instance FromJSON Transaction where
  parseJSON = withObject "Transaction" $ \v ->
    Transaction <$> v .:  "id"
                <*> v .:  "timestamp"
                <*> v .:  "source"
                <*> v .:  "chain"
                <*> v .:  "tx_type"
                <*> v .:  "wallet"
                <*> v .:? "counterparty"
                <*> v .:? "sent"
                <*> v .:? "received"
                <*> v .:? "fee"
                <*> v .:? "raw_type"
                <*> v .:? "market"
                <*> v .:? "event_group_id"
                <*> v .:? "split_reason"
                <*> v .:? "closed_pnl"

instance ToJSON Transaction where
  toJSON tx = object $
    [ "id"           .= txId tx
    , "timestamp"    .= txTimestamp tx
    , "source"       .= txSource tx
    , "chain"        .= txChain tx
    , "tx_type"      .= txType tx
    , "wallet"       .= txWallet tx
    , "counterparty" .= txCounterparty tx
    , "sent"         .= txSent tx
    , "received"     .= txReceived tx
    , "fee"          .= txFee tx
    , "raw_type"     .= txRawType tx
    ]
    ++ maybe [] (\m -> ["market" .= m]) (txMarket tx)
    ++ maybe [] (\g -> ["event_group_id" .= g]) (txEventGroupId tx)
    ++ maybe [] (\r -> ["split_reason" .= r]) (txSplitReason tx)
    ++ maybe [] (\p -> ["closed_pnl" .= p]) (txClosedPnl tx)

-- ---------------------------------------------------------------------------
-- Financial core types (internal to the engine, not from JSON)
-- ---------------------------------------------------------------------------

data TaxLot = TaxLot
  { lotAsset       :: AssetSymbol
  , lotAcquired    :: UTCTime
  , lotRemaining   :: TokenAmount
  , lotCostPerUnit :: Rational     -- USD cost basis per unit (a rate, not a total)
  } deriving (Show, Eq)

data Disposal = Disposal
  { dispAsset    :: AssetSymbol
  , dispTime     :: UTCTime
  , dispAmount   :: TokenAmount
  , dispProceeds :: USD
  , dispFee      :: USD
  } deriving (Show, Eq)

data GainLoss = GainLoss
  { glAsset       :: AssetSymbol
  , glAcquired    :: UTCTime
  , glDisposed    :: UTCTime
  , glAmount      :: TokenAmount
  , glCostBasis   :: USD
  , glProceeds    :: USD
  , glGain        :: USD           -- proceeds - costBasis (negative = loss)
  , glPeriod      :: HoldingPeriod
  } deriving (Show, Eq)

data HoldingPeriod = ShortTerm | LongTerm
  deriving (Show, Eq)

-- ---------------------------------------------------------------------------
-- Supplemental output types — not 8949 disposals
-- ---------------------------------------------------------------------------

-- | A structured income receipt report row, kept separate from 8949 disposals.
data IncomeEntry = IncomeEntry
  { iiTimestamp :: UTCTime
  , iiTxId      :: Text
  , iiAsset     :: AssetSymbol
  , iiAmount    :: TokenAmount
  , iiUSDValue  :: USD
  , iiMarket    :: Maybe Text  -- source market context (e.g. Hyperliquid coin)
  } deriving (Show, Eq)

-- | A negative funding cash flow, kept separate from 8949 disposals and perp PnL.
data FundingExpense = FundingExpense
  { feTimestamp :: UTCTime
  , feTxId      :: Text
  , feAsset     :: AssetSymbol
  , feAmount    :: TokenAmount
  , feUSDValue  :: USD
  , feMarket    :: Maybe Text  -- source market context (e.g. Hyperliquid coin)
  } deriving (Show, Eq)

-- | Direction of a wallet transfer relative to the reporting wallet.
data TransferDirection = DirIn | DirOut
  deriving (Show, Eq)

-- | A non-taxable transfer row, preserved for visibility instead of being
-- silently dropped. Transfers never touch FIFO lots.
data TransferEntry = TransferEntry
  { teTimestamp    :: UTCTime
  , teTxId         :: Text
  , teDirection    :: TransferDirection
  , teAsset        :: AssetSymbol
  , teAmount       :: TokenAmount
  , teUSDValue     :: USD
  , teWallet       :: Text
  , teCounterparty :: Maybe Text
  } deriving (Show, Eq)

-- | A realized perp PnL entry derived from the exchange's ClosedPnl field.
-- This is intentionally separate from 8949 output because the cost-basis and
-- proceeds representation for derivative PnL on Form 8949 is unresolved.
data PerpPnlEntry = PerpPnlEntry
  { ppTimestamp :: UTCTime
  , ppTxId      :: Text
  , ppAsset     :: AssetSymbol
  , ppAmount    :: TokenAmount
  , ppDirection :: Text         -- raw_type from IR (e.g. "Close Long")
  , ppClosedPnl :: USD          -- exchange-reported realized PnL
  } deriving (Show, Eq)
