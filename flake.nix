{
  description = "Mindful Social — community platform for structured discourse";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    # nixosModules is not per-system; it's a single entry point users
    # import from their NixOS configuration. The module itself defaults
    # services.mindful-social.package via pkgs.callPackage, so a typical
    # user only has to flip enable = true.
    {
      nixosModules.default = ./nix/module.nix;
      nixosModules.mindful-social = self.nixosModules.default;
    }
    //
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;

        mindful-social = pkgs.callPackage ./nix/package.nix {};
      in {
        packages = {
          default = mindful-social;
          mindful-social = mindful-social;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            sqlc
            goose
            templ
            air
            postgresql_16
            golangci-lint

            # TTS sidecar (./tts): Python interpreter, uv for the venv, and
            # espeak-ng for Kokoro's phonemizer. The Python packages
            # themselves (kokoro_onnx, onnxruntime, fastapi, ...) live in a
            # uv-managed venv at tts/.venv — see tts/setup.sh.
            python312
            uv
            espeak-ng
          ]
          # ffmpeg is used by the node-video upload handler AND by the TTS
          # sidecar (Opus encoding of synthesized audio). On Linux we ship
          # the headless build; on macOS we defer to the system install
          # (Homebrew etc.) because nix-built ffmpeg binaries are killed
          # by Apple's hardened-runtime policy on recent macOS releases.
          ++ lib.optional (!pkgs.stdenv.isDarwin) pkgs.ffmpeg-headless;

          shellHook = ''
            export PROJECT_ROOT="$PWD"
            export PGDATA="$PROJECT_ROOT/.pgdata"
            export PGHOST="$PROJECT_ROOT/.pgrun"
            export PGPORT=5433
            export PGDATABASE=mindful_social
            export DATABASE_URL="postgres:///$PGDATABASE?host=$PGHOST&port=$PGPORT"
            export TEST_DATABASE_URL="postgres:///mindful_social_test?host=$PGHOST&port=$PGPORT"
            export DATA_DIR="$PROJECT_ROOT"

            mkdir -p "$PGHOST"

            cat <<EOF

            Mindful Social — dev shell
              go:        $(go version | awk '{print $3}')
              postgres:  PGDATA=$PGDATA  PGHOST=$PGHOST  PGPORT=$PGPORT
              database:  $DATABASE_URL

            First-time setup:
              ./scripts/db-init.sh         initialize Postgres data dir
              ./scripts/db-start.sh        start Postgres in foreground (Ctrl-C to stop)
              ./scripts/db-create.sh       create the mindful_social database (in another shell)
              ./scripts/migrate-up.sh      apply migrations
              ./scripts/db-test-setup.sh   create + migrate the test database (optional; for go test)

            Run the app:
              go run ./cmd/server          listens on 127.0.0.1:8080

            TTS sidecar (optional):
              ./tts/setup.sh               install Python deps + download Kokoro models
              ./tts/run.sh                 start the sidecar on 127.0.0.1:8090

            EOF
          '';
        };
      });
}
