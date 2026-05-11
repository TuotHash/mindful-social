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
        lib = pkgs.lib;

        # Source filter for the Nix package build. Strips dev-only
        # artefacts (Postgres data dir, the design sandbox, archives)
        # that would otherwise bloat the store derivation or, in the
        # case of redesign/, break go build.
        packageSrc = lib.cleanSourceWith {
          src = lib.cleanSource ./.;
          filter = path: type:
            let baseName = baseNameOf (toString path); in
              !(baseName == ".pgdata")
              && !(baseName == ".pgrun")
              && !(baseName == "redesign")
              && !(lib.hasSuffix ".zip" baseName);
        };

        mindful-social = pkgs.buildGoModule {
          pname = "mindful-social";
          version = "1.0.0-alpha";
          src = packageSrc;

          # First-build flow: set this to lib.fakeHash, run `nix build`,
          # then replace with the hash the failed build prints.
          vendorHash = "sha256-WP/Kk6n3Zw1JbmboPyIv6BOuKrZTaA1qHbiZhv1w1+U=";

          subPackages = [ "cmd/server" ];

          # buildGoModule names the binary after the last path component
          # of subPackages, which would be "server". Rename to the
          # project name so systemd units and PATH lookups read naturally.
          postInstall = ''
            mv $out/bin/server $out/bin/mindful-social
          '';

          meta = {
            description = "Community platform combining free-form discussion with a typed argument graph";
            homepage = "https://github.com/TuotHash/mindful-social";
            license = lib.licenses.agpl3Only;
            mainProgram = "mindful-social";
            platforms = lib.platforms.unix;
          };
        };
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
          ];

          shellHook = ''
            export PROJECT_ROOT="$PWD"
            export PGDATA="$PROJECT_ROOT/.pgdata"
            export PGHOST="$PROJECT_ROOT/.pgrun"
            export PGPORT=5433
            export PGDATABASE=mindful_social
            export DATABASE_URL="postgres:///$PGDATABASE?host=$PGHOST&port=$PGPORT"
            export TEST_DATABASE_URL="postgres:///mindful_social_test?host=$PGHOST&port=$PGPORT"

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

            EOF
          '';
        };
      });
}
