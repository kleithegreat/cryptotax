{
  description = "cryptotax - Crypto tax report generator (Go plumbing + Haskell financial core)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        haskellPackages = pkgs.haskellPackages;

        # Haskell binary
        cryptotax-core = haskellPackages.callCabal2nix "cryptotax-core" ./haskell { };

        # Go binary
        cryptotax-cli = pkgs.buildGoModule {
          pname = "cryptotax-cli";
          version = "0.1.0";
          src = ./go;
          # vendorHash must be updated after any go.mod change.
          # Set to null for initial bootstrap; nix build will report the
          # correct hash on first failure — paste it here to fix.
          vendorHash = null;
        };
      in
      {
        packages = {
          core = cryptotax-core;
          cli = cryptotax-cli;
          default = cryptotax-cli;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain
            go
            gopls
            gotools
            go-tools # staticcheck

            # Haskell toolchain
            ghc
            cabal-install
            haskell-language-server
            haskellPackages.QuickCheck
            haskellPackages.aeson
            haskellPackages.text
            haskellPackages.bytestring
            haskellPackages.containers
            haskellPackages.time
            haskellPackages.vector

            # Shared tooling
            jq
            gnumake
          ];

          shellHook = ''
            echo "cryptotax dev shell"
            echo "  Go:      $(go version)"
            echo "  GHC:     $(ghc --version)"
            echo "  Cabal:   $(cabal --version | head -1)"
          '';
        };
      });
}
