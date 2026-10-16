{-# LANGUAGE LambdaCase        #-}
{-# LANGUAGE OverloadedStrings #-}

-- | The IR the Go side produces (go/ledger/types.go) and the core's own
-- result types. Every quantity is an exact 'Rational'.
module Types
  ( Payload (..)
  , Event (..)
  , Kind (..)
  , Leg (..)
  , PerpFill (..)
  , PerpPart (..)
  , Broker (..)
  , Issue (..)
  , Term (..)
  , Row (..)
  , Origin (..)
  , IncomeRow (..)
  , isCash
  , parseDecimal
  ) where

import           Data.Aeson
import           Data.Aeson.Types (Parser)
import           Data.Ratio       ((%))
import           Data.Text        (Text)
import qualified Data.Text        as T
import           Data.Time        (Day, UTCTime, defaultTimeLocale, parseTimeM)

data Payload = Payload
  { pVersion :: Text
  , pEvents  :: [Event]
  , pIssues  :: [Issue]
  , pYears   :: [Integer]
  }

data Event = Event
  { evId       :: Text
  , evTime     :: UTCTime
  , evDate     :: Day
  , evKind     :: Kind
  , evLegs     :: [Leg]
  , evFees     :: [Leg]
  , evReason   :: Text
  , evBasis    :: Maybe Rational
  , evAcquired :: Maybe Day
  , evNote     :: Text
  }

data Kind
  = Trade
  | Move
  | Income
  | Spend
  | Add
  | Remove
  | Perp PerpFill
  | Funding Text Text Rational -- ^ account, market, USD (+ received)
  | BrokerSale Broker

data Leg = Leg
  { legAccount :: Text
  , legAsset   :: Text
  , legSymbol  :: Text
  , legAmount  :: Rational -- ^ signed
  , legUSD     :: Maybe Rational
  }

data PerpFill = PerpFill
  { pfAccount :: Text
  , pfMarket  :: Text
  , pfClose   :: Maybe PerpPart
  , pfOpen    :: Maybe PerpPart
  , pfPnL     :: Rational
  , pfFee     :: Rational
  }

data PerpPart = PerpPart
  { ppSide :: Text
  , ppSize :: Rational
  }

data Broker = Broker
  { brAccount       :: Text
  , brAsset         :: Text
  , brSymbol        :: Text
  , brAmount        :: Rational
  , brProceeds      :: Rational
  , brBasis         :: Maybe Rational
  , brTerm          :: Maybe Term
  , brBasisReported :: Bool
  }

data Issue = Issue
  { isEvent    :: Text
  , isBlocking :: Bool
  , isMessage  :: Text
  }

data Term = Short | Long
  deriving (Eq, Ord, Show)

-- | Where a Form 8949 row comes from; decides the check box.
data Origin = Unreported | BrokerReported Bool -- ^ basis reported to the IRS?
  deriving (Eq, Ord, Show)

-- | One Form 8949 line. 'rAcquired' is Nothing for "VARIOUS".
data Row = Row
  { rOrigin   :: Origin
  , rTerm     :: Term
  , rAsset    :: Text
  , rAmount   :: Rational
  , rAcquired :: Maybe Day
  , rSold     :: Day
  , rProceeds :: Rational
  , rBasis    :: Rational
  , rAccount  :: Text
  , rEvent    :: Text
  , rMerged   :: Int -- ^ how many disposals this line consolidates
  } deriving (Show)

data IncomeRow = IncomeRow
  { iDate    :: Day
  , iType    :: Text
  , iAsset   :: Text
  , iAmount  :: Rational
  , iUSD     :: Rational
  , iAccount :: Text
  , iEvent   :: Text
  }

-- | US-dollar stablecoins are cash: no lots, no gain (docs/assets.md).
isCash :: Text -> Bool
isCash = (== "USD")

-- | Parse the IR's canonical decimal: optional '-', digits, optional '.'
-- and digits. Anything else is a contract violation.
parseDecimal :: Text -> Either String Rational
parseDecimal t = case T.uncons t of
  Just ('-', rest) -> negate <$> unsigned rest
  _                -> unsigned t
  where
    unsigned s = case T.splitOn "." s of
      [w] | digits w -> Right (fromInteger (readI w))
      [w, f] | digits w && digits f -> Right (fromInteger (readI w) + readI f % (10 ^ T.length f))
      _ -> Left ("malformed decimal " ++ show t)
    digits s = not (T.null s) && T.all (\c -> c >= '0' && c <= '9') s
    readI = T.foldl' (\a c -> a * 10 + toInteger (fromEnum c - fromEnum '0')) 0

decimal :: Value -> Parser Rational
decimal = withText "decimal" (either fail pure . parseDecimal)

day :: Text -> Parser Day
day = parseTimeM False defaultTimeLocale "%Y-%m-%d" . T.unpack

instance FromJSON Payload where
  parseJSON = withObject "Payload" $ \o ->
    Payload <$> o .: "version" <*> o .: "events" <*> o .: "issues" <*> o .: "years"

instance FromJSON Issue where
  parseJSON = withObject "Issue" $ \o ->
    Issue <$> o .: "event" <*> o .: "blocking" <*> o .: "message"

instance FromJSON Leg where
  parseJSON = withObject "Leg" $ \o ->
    Leg <$> o .: "account" <*> o .: "asset" <*> o .: "symbol"
        <*> (o .: "amount" >>= decimal)
        <*> (o .:? "usd" >>= traverse decimal)

instance FromJSON Event where
  parseJSON = withObject "Event" $ \o -> do
    kind <- o .: "kind" >>= \case
      "trade"   -> pure Trade
      "move"    -> pure Move
      "income"  -> pure Income
      "spend"   -> pure Spend
      "add"     -> pure Add
      "remove"  -> pure Remove
      "perp"    -> Perp <$> (o .: "perp" >>= perp)
      "funding" -> o .: "funding" >>= withObject "funding" (\f ->
                     Funding <$> f .: "account" <*> f .: "market" <*> (f .: "usd" >>= decimal))
      "broker_sale" -> BrokerSale <$> (o .: "broker" >>= broker)
      other -> fail ("unknown event kind " ++ T.unpack other)
    Event <$> o .: "id" <*> o .: "time" <*> (o .: "date" >>= day) <*> pure kind
          <*> o .: "legs" <*> o .: "fees"
          <*> (maybe "" id <$> o .:? "reason")
          <*> (o .:? "basis" >>= traverse decimal)
          <*> (o .:? "acquired" >>= traverse day)
          <*> (maybe "" id <$> o .:? "note")
    where
      perp = withObject "perp" $ \p ->
        PerpFill <$> p .: "account" <*> p .: "market"
                 <*> (p .:? "close" >>= traverse part) <*> (p .:? "open" >>= traverse part)
                 <*> (p .: "pnl" >>= decimal) <*> (p .: "fee" >>= decimal)
      part = withObject "part" $ \p -> PerpPart <$> p .: "side" <*> (p .: "size" >>= decimal)
      broker = withObject "broker" $ \b ->
        Broker <$> b .: "account" <*> b .: "asset" <*> b .: "symbol"
               <*> (b .: "amount" >>= decimal) <*> (b .: "proceeds" >>= decimal)
               <*> (b .:? "basis" >>= traverse decimal)
               <*> (b .: "term" >>= term) <*> b .: "basis_reported"
      term :: Text -> Parser (Maybe Term)
      term = \case
        "short" -> pure (Just Short)
        "long"  -> pure (Just Long)
        ""      -> pure Nothing
        other   -> fail ("unknown term " ++ T.unpack other)
