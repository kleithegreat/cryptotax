{-# LANGUAGE OverloadedStrings #-}

module Main (main) where

import           Control.Monad        (forM_, unless, when)
import qualified Data.Aeson           as Aeson
import qualified Data.ByteString      as BS
import qualified Data.ByteString.Lazy as BL
import qualified Data.Map.Strict      as Map
import           Data.Text            (Text)
import qualified Data.Text            as T
import           Data.Text.Encoding   (encodeUtf8)
import qualified Data.Text.IO         as TIO
import           Data.Time            (toGregorian)
import           System.Directory     (createDirectoryIfMissing)
import           System.Environment   (getArgs)
import           System.Exit          (ExitCode (..), exitWith)
import           System.FilePath      ((</>))
import           System.IO            (stderr)

import           Engine
import           Report
import           Types

main :: IO ()
main = do
  args <- getArgs
  let out = case dropWhile (/= "--out") args of
        (_ : d : _) -> d
        _           -> "reports"
  input <- BL.getContents
  payload <- case Aeson.eitherDecode input of
    Left err -> die' ("cannot parse IR: " <> T.pack err)
    Right p  -> pure p
  unless (pVersion payload == "2") $ die' ("unsupported IR version " <> pVersion payload)

  let res = run (pYears payload) (pEvents payload)
      issues = pIssues payload ++ resIssues res
      blocking = length (filter isBlocking issues)
      symbols = Map.fromList [ (legAsset l, legSymbol l) | e <- pEvents payload, l <- evLegs e ++ evFees e ]
      sym a = Map.findWithDefault a a symbols
      yearOf d = let (y, _, _) = toGregorian d in y
      rows = consolidate (resRows res)

  createDirectoryIfMissing True out
  write (out </> "review.csv") (reviewCSV issues)
  write (out </> "exported-lots.csv") (exportedCSV sym (resExported res))
  forM_ (pYears payload) $ \y -> do
    let dir = out </> show y
        rs = [ r | r <- rows, yearOf (rSold r) == y ]
        is = [ i | i <- resIncome res, yearOf (iDate i) == y ]
        s = summary y blocking rs is
    createDirectoryIfMissing True dir
    write (dir </> "form8949.csv") (form8949 y rs)
    write (dir </> "income.csv") (incomeCSV is)
    write (dir </> "holdings-end.csv") (holdingsCSV sym (Map.findWithDefault [] y (resHoldings res)))
    write (dir </> "summary.txt") s
    TIO.hPutStrLn stderr s

  TIO.hPutStrLn stderr $ "Reports written to " <> T.pack out <> "/"
  when (blocking > 0) $ do
    TIO.hPutStrLn stderr $ "PROVISIONAL: " <> T.pack (show blocking) <> " blocking item(s) in review.csv"
    exitWith (ExitFailure 3)

write :: FilePath -> Text -> IO ()
write path = BS.writeFile path . encodeUtf8

die' :: Text -> IO a
die' msg = TIO.hPutStrLn stderr msg >> exitWith (ExitFailure 1)
