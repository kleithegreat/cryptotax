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

parseDecimal :: Text -> Rational
parseDecimal t =
  case T.splitOn "." t of
    [whole]       -> fromIntegral (readInt whole)
    [whole, frac] ->
      let w     = readInt whole
          f     = readUnsigned frac
          scale = 10 ^ T.length frac
          sign  = if T.isPrefixOf "-" whole then -1 else 1
       in fromIntegral w + sign * (f % scale)
    _ -> error $ "parseDecimal: invalid input: " ++ T.unpack t
  where
    readInt :: Text -> Integer
    readInt s = case T.uncons s of
      Just ('-', rest) -> negate (readUnsigned rest)
      _                -> readUnsigned s

    readUnsigned :: Text -> Integer
    readUnsigned = T.foldl' (\acc c -> acc * 10 + fromIntegral (fromEnum c - fromEnum '0')) 0

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

data AssetAmount = AssetAmount
  { aaAsset    :: Text
  , aaAmount   :: Text     -- parsed to Rational in the core
  , aaUSDValue :: Text     -- parsed to Rational in the core
  } deriving (Show, Eq, Generic)

instance FromJSON AssetAmount where
  parseJSON = withObject "AssetAmount" $ \v ->
    AssetAmount <$> v .: "asset"
                <*> v .: "amount"
                <*> v .: "usd_value"

instance ToJSON AssetAmount where
  toJSON a = object
    [ "asset"     .= aaAsset a
    , "amount"    .= aaAmount a
    , "usd_value" .= aaUSDValue a
    ]

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

instance ToJSON Transaction where
  toJSON tx = object
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
