{
  description = "cryptotax - Crypto tax report generator (Go plumbing + Haskell core)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;
        hsPkgs = pkgs.haskellPackages;
        commonMeta = {
          license = lib.licenses.mit;
          platforms = lib.platforms.unix;
        };

        devShell = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            go-tools
            ghc
            cabal-install
            haskell-language-server
            haskellPackages.QuickCheck
            jq
          ];
        };

        core = (hsPkgs.callCabal2nix "cryptotax-core" ./haskell { }).overrideAttrs (old: {
          meta = (old.meta or { }) // commonMeta // {
            mainProgram = "cryptotax-core";
            description = "Haskell financial core for crypto tax lot accounting";
          };
        });

        cli = pkgs.buildGoModule {
          pname = "cryptotax";
          version = "0.1.0";
          src = ./.;
          modRoot = "./go";
          subPackages = [ "cmd" ];
          vendorHash = "sha256-hocnLCzWN8srQcO3BMNkd2lt0m54Qe7sqAhUxVZlz1k=";
          postInstall = ''
            if [ -e "$out/bin/cmd" ]; then
              mv "$out/bin/cmd" "$out/bin/cryptotax"
            fi
          '';
          meta = commonMeta // {
            mainProgram = "cryptotax";
            description = "Crypto tax report generator";
          };
        };

        bundle = pkgs.symlinkJoin {
          name = "cryptotax-bundle";
          paths = [ cli core ];
          meta = commonMeta // {
            description = "Combined cryptotax CLI and Haskell core";
          };
        };

        runApp = pkgs.writeShellApplication {
          name = "cryptotax-run";
          runtimeInputs = [ cli core ];
          text = ''
            exec ${lib.getExe cli} run --core ${lib.getExe core} "$@"
          '';
        };

        dryRunApp = pkgs.writeShellApplication {
          name = "cryptotax-dry-run";
          runtimeInputs = [ cli ];
          text = ''
            exec ${lib.getExe cli} run --dry-run "$@"
          '';
        };

        auditApp = pkgs.writeShellApplication {
          name = "cryptotax-audit";
          runtimeInputs = [ cli ];
          text = ''
            export CRYPTOTAX_AUDIT_RERUN_PREFIX='nix run .#audit --'
            exec ${lib.getExe cli} audit "$@"
          '';
        };
      in {
        packages = {
          inherit cli core bundle;
          default = bundle;
        };

        apps = {
          cli = {
            type = "app";
            program = lib.getExe cli;
            meta = commonMeta // {
              description = "Run the cryptotax Go CLI";
            };
          };
          core = {
            type = "app";
            program = lib.getExe core;
            meta = commonMeta // {
              description = "Run the cryptotax Haskell financial core";
            };
          };
          run = {
            type = "app";
            program = "${runApp}/bin/cryptotax-run";
            meta = commonMeta // {
              description = "Run the Go CLI with the flake-built Haskell core";
            };
          };
          dry-run = {
            type = "app";
            program = "${dryRunApp}/bin/cryptotax-dry-run";
            meta = commonMeta // {
              description = "Run the Go CLI in --dry-run mode";
            };
          };
          audit = {
            type = "app";
            program = "${auditApp}/bin/cryptotax-audit";
            meta = commonMeta // {
              description = "Run the Go CLI audit subcommands";
            };
          };
          default = {
            type = "app";
            program = "${runApp}/bin/cryptotax-run";
            meta = commonMeta // {
              description = "Run the Go CLI with the flake-built Haskell core";
            };
          };
        };

        checks = {
          go-build = cli;
          core-build = core;
          hs-tests = pkgs.haskell.lib.doCheck core;
        };

        devShells.default = devShell;
      });
}
