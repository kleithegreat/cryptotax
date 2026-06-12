{-# LANGUAGE OverloadedStrings #-}

module Main (main) where

import           Control.Monad        (unless, when)
import qualified Data.Aeson           as Aeson
import qualified Data.ByteString      as BS
import qualified Data.ByteString.Lazy as BL
import           Data.Text            (Text)
import qualified Data.Text            as T
import           Data.Text.Encoding   (encodeUtf8)
import           Data.Time            (toGregorian, utctDay)
import           System.Directory     (doesFileExist, removeFile)
import           System.Environment   (getArgs)
import           System.Exit          (exitFailure)
import           System.IO            (hPutStrLn, stderr)
import           Text.Read            (readMaybe)

import           System.FilePath (takeDirectory, (</>))

import           Types
import           GainLoss  (processTransactions, ProcessResult(..))
import           Report    (render8949CSV, renderIncomeCSV, renderFundingExpenseCSV,
                            renderPerpPnlCSV, renderTransfersCSV, renderUSD)

-- | The only payload contract version this core understands.
supportedVersion :: Text
supportedVersion = "1.0.0"

main :: IO ()
main = do
  args <- getArgs
  let outputFile = parseOutputFlag args
  taxYear <- parseTaxYearFlag args

  input <- BL.getContents

  case Aeson.eitherDecode input of
    Left err -> do
      hPutStrLn stderr $ "Failed to parse JSON input: " ++ err
      exitFailure

    Right payload -> do
      unless (payloadVersion payload == supportedVersion) $ do
        hPutStrLn stderr $ "Unsupported payload version: "
          ++ show (payloadVersion payload)
          ++ " (expected " ++ show supportedVersion ++ ")"
        exitFailure

      hPutStrLn stderr $ "Received "
        ++ show (length (payloadTransactions payload))
        ++ " transactions, "
        ++ show (length (payloadWallets payload))
        ++ " wallets"

      let result = processTransactions (payloadTransactions payload)

      mapM_ (\e -> hPutStrLn stderr $ "Warning: " ++ show e) (prErrors result)

      -- Lot accounting always runs over the full history; --tax-year only
      -- filters which rows are rendered into the report files.
      let inYear t = case taxYear of
            Nothing -> True
            Just y  -> let (ty, _, _) = toGregorian (utctDay t) in ty == y
          gains         = filter (inYear . glDisposed)  (prGainLosses result)
          incomeEntries = filter (inYear . iiTimestamp) (prIncome result)
          fundingExps   = filter (inYear . feTimestamp) (prFundingExpenses result)
          perpEntries   = filter (inYear . ppTimestamp) (prPerpPnl result)
          transferRows  = filter (inYear . teTimestamp) (prTransfers result)

      case taxYear of
        Nothing -> pure ()
        Just y -> do
          let kept  = length gains + length incomeEntries + length fundingExps
                    + length perpEntries + length transferRows
              total = length (prGainLosses result) + length (prIncome result)
                    + length (prFundingExpenses result) + length (prPerpPnl result)
                    + length (prTransfers result)
          hPutStrLn stderr $ "Tax year " ++ show y ++ ": excluded "
            ++ show (total - kept) ++ " row(s) outside the requested year"

      let csv = render8949CSV gains
      writeFileUtf8 outputFile csv

      hPutStrLn stderr $ "Wrote "
        ++ show (length gains)
        ++ " gain/loss events to "
        ++ outputFile

      let totalGain = sum $ map (unUSD . glGain) gains
      hPutStrLn stderr $ "Net gain/loss: $" ++ T.unpack (renderUSD (USD totalGain))

      let incomeTotal = sum $ map (unUSD . iiUSDValue) incomeEntries
      hPutStrLn stderr $ "Total income: $" ++ T.unpack (renderUSD (USD incomeTotal))

      let outDir = takeDirectory outputFile

      -- Supplemental: ordinary income report
      writeSupplemental (outDir </> "income.csv") "income"
        (renderIncomeCSV incomeEntries) (length incomeEntries)

      -- Supplemental: funding expenses report
      unless (null fundingExps) $ do
        let fundingTotal = sum $ map (unUSD . feUSDValue) fundingExps
        hPutStrLn stderr $ "Total funding expenses: $"
          ++ T.unpack (renderUSD (USD fundingTotal))
      writeSupplemental (outDir </> "funding_expenses.csv") "funding expense"
        (renderFundingExpenseCSV fundingExps) (length fundingExps)

      -- Supplemental: perp realized PnL report
      unless (null perpEntries) $ do
        let perpTotal = sum $ map (unUSD . ppClosedPnl) perpEntries
        hPutStrLn stderr $ "Total perp realized PnL: $"
          ++ T.unpack (renderUSD (USD perpTotal))
      writeSupplemental (outDir </> "perp_pnl.csv") "perp PnL"
        (renderPerpPnlCSV perpEntries) (length perpEntries)

      -- Supplemental: transfer visibility report (non-taxable activity)
      writeSupplemental (outDir </> "transfers.csv") "transfer"
        (renderTransfersCSV transferRows) (length transferRows)

      -- Report files are written above even when rows failed, but the exit
      -- code must still signal that the 8949 is incomplete.
      unless (null (prErrors result)) $ do
        hPutStrLn stderr $ "ERROR: " ++ show (length (prErrors result))
          ++ " transaction(s) could not be processed; the 8949 report is incomplete"
        exitFailure

-- | Write report output as UTF-8 regardless of the process locale.
writeFileUtf8 :: FilePath -> Text -> IO ()
writeFileUtf8 path = BS.writeFile path . encodeUtf8

-- | Write a supplemental CSV when its channel has rows; remove a stale file
-- left by a previous run when the channel is empty.
writeSupplemental :: FilePath -> String -> Text -> Int -> IO ()
writeSupplemental path label csv rowCount
  | rowCount == 0 = do
      stale <- doesFileExist path
      when stale (removeFile path)
  | otherwise = do
      writeFileUtf8 path csv
      hPutStrLn stderr $ "Wrote " ++ show rowCount ++ " " ++ label
        ++ " rows to " ++ path

parseOutputFlag :: [String] -> FilePath
parseOutputFlag []                   = "8949_report.csv"
parseOutputFlag ("--output" : f : _) = f
parseOutputFlag (_ : rest)           = parseOutputFlag rest

-- | Parse the optional --tax-year flag. Lot accounting still processes the
-- full history; the flag only controls which rows are rendered.
parseTaxYearFlag :: [String] -> IO (Maybe Integer)
parseTaxYearFlag []                  = pure Nothing
parseTaxYearFlag ["--tax-year"]      = badTaxYear "(missing value)"
parseTaxYearFlag ("--tax-year" : v : _) =
  case readMaybe v :: Maybe Integer of
    Just y | y >= 1000 && y <= 9999 -> pure (Just y)
    _                               -> badTaxYear v
parseTaxYearFlag (_ : rest)          = parseTaxYearFlag rest

badTaxYear :: String -> IO a
badTaxYear v = do
  hPutStrLn stderr $ "Invalid --tax-year value: " ++ v ++ " (expected a 4-digit year)"
  exitFailure
