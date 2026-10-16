{
  description = "cryptotax — US crypto tax reports (Go source plumbing + Haskell lot accounting)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        inherit (pkgs) lib;
        meta = {
          license = lib.licenses.mit;
          platforms = lib.platforms.unix;
        };

        core = (pkgs.haskellPackages.callCabal2nix "cryptotax-core" ./haskell { }).overrideAttrs (old: {
          meta = (old.meta or { }) // meta // {
            mainProgram = "cryptotax-core";
            description = "Lot accounting and tax reports for the cryptotax IR";
          };
        });

        # The Go module has no dependencies, so there is nothing to vendor;
        # doCheck runs the whole Go test suite as part of the build.
        cli = pkgs.buildGoModule {
          pname = "cryptotax";
          version = "0.2.0";
          src = ./go;
          vendorHash = null;
          subPackages = [ "cmd/cryptotax" ];
          preCheck = ''
            unset subPackages
          '';
          meta = meta // {
            mainProgram = "cryptotax";
            description = "Fetch, classify and report crypto tax history";
          };
        };

        # `nix run` entry: the CLI with the core it was built with.
        cryptotax = pkgs.writeShellApplication {
          name = "cryptotax";
          runtimeInputs = [ cli core ];
          text = ''
            sub="''${1:-}"
            shift || true
            exec ${lib.getExe cli} "$sub" --core ${lib.getExe core} "$@"
          '';
          meta = meta // { description = "cryptotax with its Haskell core"; };
        };
      in {
        packages = {
          inherit cli core cryptotax;
          default = cryptotax;
        };

        apps.default = {
          type = "app";
          program = lib.getExe cryptotax;
          meta = meta // { description = "cryptotax report|events"; };
        };

        checks = {
          inherit cli;
          core = pkgs.haskell.lib.doCheck core;
        };

        devShells.default = pkgs.haskellPackages.shellFor {
          packages = _: [ core ];
          nativeBuildInputs = with pkgs; [
            go
            gopls
            gotools
            cabal-install
            haskell-language-server
            jq
          ];
        };
      });
}
