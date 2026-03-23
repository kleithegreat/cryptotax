.PHONY: all build-go build-hs test-hs test clean run dry-run

all: build-go build-hs

# --- Go plumbing layer ---
build-go:
	cd go && go build -o ../bin/cryptotax ./cmd

# --- Haskell financial core ---
build-hs:
	cd haskell && cabal build
	cp $$(cd haskell && cabal list-bin cryptotax-core) bin/cryptotax-core

# --- Tests ---
test-hs:
	cd haskell && cabal test --test-show-details=streaming

test: test-hs

# --- Run ---
# Example: make run ETH_WALLET=0x... SOL_WALLET=... ROBINHOOD_CSV=data/rh.csv
run: all
	./bin/cryptotax run \
		$$([ -n "$(ETH_WALLET)" ] && echo "--eth-wallet $(ETH_WALLET)") \
		$$([ -n "$(SOL_WALLET)" ] && echo "--sol-wallet $(SOL_WALLET)") \
		$$([ -n "$(HL_WALLET)" ] && echo "--hl-wallet $(HL_WALLET)") \
		$$([ -n "$(ROBINHOOD_CSV)" ] && echo "--robinhood-csv $(ROBINHOOD_CSV)") \
		--core ./bin/cryptotax-core \
		--output 8949_report.csv

# Dry run: emit normalized JSON without running the Haskell core
dry-run: build-go
	./bin/cryptotax run \
		$$([ -n "$(ETH_WALLET)" ] && echo "--eth-wallet $(ETH_WALLET)") \
		$$([ -n "$(SOL_WALLET)" ] && echo "--sol-wallet $(SOL_WALLET)") \
		$$([ -n "$(HL_WALLET)" ] && echo "--hl-wallet $(HL_WALLET)") \
		$$([ -n "$(ROBINHOOD_CSV)" ] && echo "--robinhood-csv $(ROBINHOOD_CSV)") \
		--dry-run

clean:
	rm -rf bin/
	cd go && go clean
	cd haskell && cabal clean

# Create bin directory
bin:
	mkdir -p bin
build-go: | bin
build-hs: | bin
