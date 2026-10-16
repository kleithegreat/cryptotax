package robinhood

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Rows shaped like a real 2025 Robinhood consolidated 1099 export (account
// numbers and amounts are made up).
const export = `1099-DIV,ACCOUNT NUMBER,TAX YEAR,ORDINARY DIV
1099-DIV,111,2025,1.00
1099-DA,ACCOUNT NUMBER,TAX YEAR,DATE ACQUIRED,SALE DATE,DESCRIPTION,DTIF CODE,DTIF NAME,DTIF UNITS,COST BASIS,SALES PRICE,TERM,FORM8949CODE,NON COVERED,AGGREGATE TRANS IND,DA NUM UNITS TRANS IN,DA TRANSFER IN DATE,STABLECOIN,AGG NUM TRANS
1099-DA,111C,2025,,20250915,Solana SOL,20J63Z4N3,Solana SOL,1.5,300.00,330.00,SHORT,H,1,Y,,,,
1099-DA,111C,2025,,20250915,Solana SOL,20J63Z4N3,Solana SOL,1e-05,0,0,,X,1,,2e-05,2025-09-16,,
1099-DA,111C,2025,,,USDC,CK9PW1MFH,USDC,10.5,,10.5,,X,1,,,,1,2
1099-B,ACCOUNT NUMBER,TAX YEAR
`

func TestRead1099DA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rh.csv")
	if err := os.WriteFile(path, []byte(export), 0o644); err != nil {
		t.Fatal(err)
	}
	ny, _ := time.LoadLocation("America/New_York")
	evs, err := Read(path, ny)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("%d events", len(evs))
	}
	own, transferred, cash := evs[0].Broker, evs[1].Broker, evs[2].Broker
	if own.Asset != "SOL" || own.Basis == nil || *own.Basis != "300" || own.Term != "short" || own.Proceeds != "330" || own.BasisReported {
		t.Errorf("broker-basis row %+v", own)
	}
	if transferred.Basis != nil || transferred.Amount != "0.00001" || transferred.Term != "" {
		t.Errorf("transferred-in row must take basis from lots: %+v", transferred)
	}
	if cash.Asset != "USD" || evs[2].Time.In(ny).Format("2006-01-02") != "2025-12-31" {
		t.Errorf("stablecoin row %+v at %s", cash, evs[2].Time)
	}
}

func TestUnknownDTIFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rh.csv")
	bad := "1099-DA,ACCOUNT NUMBER,TAX YEAR,SALE DATE,DTIF CODE,DTIF NAME,DTIF UNITS,COST BASIS,SALES PRICE\n" +
		"1099-DA,1,2025,20250101,ZZZZZZZZZ,Bitcoin Cash,1,1,1\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, time.UTC); err == nil {
		t.Error("an unknown DTI must not be guessed from its name")
	}
}
