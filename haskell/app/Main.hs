{-# LANGUAGE OverloadedStrings #-}

module Main (main) where

import qualified Data.Aeson           as Aeson
import qualified Data.ByteString.Lazy as BL
import qualified Data.Text.IO         as TIO
import           System.Environment   (getArgs)
import           System.Exit          (exitFailure)
import           System.IO            (hPutStrLn, stderr)

import           Types
import           GainLoss  (processTransactions, ProcessResult(..))
import           Report    (render8949CSV)

main :: IO ()
main = do
  args <- getArgs
  let outputFile = parseOutputFlag args

  input <- BL.getContents

  case Aeson.eitherDecode input of
    Left err -> do
      hPutStrLn stderr $ "Failed to parse JSON input: " ++ err
      exitFailure

    Right payload -> do
      hPutStrLn stderr $ "Received "
        ++ show (length (payloadTransactions payload))
        ++ " transactions, "
        ++ show (length (payloadWallets payload))
        ++ " wallets"

      let result = processTransactions (payloadTransactions payload)

      mapM_ (\e -> hPutStrLn stderr $ "Warning: " ++ show e) (prErrors result)

      let csv = render8949CSV (prGainLosses result)
      TIO.writeFile outputFile csv

      hPutStrLn stderr $ "Wrote "
        ++ show (length (prGainLosses result))
        ++ " gain/loss events to "
        ++ outputFile

      let totalGain = sum $ map (unUSD . glGain) (prGainLosses result)
      hPutStrLn stderr $ "Net gain/loss: $" ++ show (fromRational totalGain :: Double)

      let incomeTotal = sum $ map (unUSD . glGain) (prIncome result)
      hPutStrLn stderr $ "Total income: $" ++ show (fromRational incomeTotal :: Double)

parseOutputFlag :: [String] -> FilePath
parseOutputFlag []                   = "8949_report.csv"
parseOutputFlag ("--output" : f : _) = f
parseOutputFlag (_ : rest)           = parseOutputFlag rest
