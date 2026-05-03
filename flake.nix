{
  description = "Mindful Social — community platform for structured discourse";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
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
          ];

          shellHook = ''
            export PROJECT_ROOT="$PWD"
            export PGDATA="$PROJECT_ROOT/.pgdata"
            export PGHOST="$PROJECT_ROOT/.pgrun"
            export PGPORT=5433
            export PGDATABASE=mindful_social
            export DATABASE_URL="postgres:///$PGDATABASE?host=$PGHOST&port=$PGPORT"

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

            Run the app:
              go run ./cmd/server          listens on 127.0.0.1:8080

            EOF
          '';
        };
      });
}
