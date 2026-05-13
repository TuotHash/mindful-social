# Build definition for the Mindful Social server. Kept separate from the
# flake and the NixOS module so both flake users (via `nix build`) and
# channels users (via `pkgs.callPackage`) consume the exact same recipe.
{ lib, buildGoModule }:

let
  # Strip dev-only artefacts (Postgres data dir, the design sandbox,
  # archives) that would otherwise bloat the store derivation or, in
  # the case of redesign/, break `go build`.
  src = lib.cleanSourceWith {
    src = lib.cleanSource ../.;
    filter = path: type:
      let baseName = baseNameOf (toString path); in
        !(baseName == ".pgdata")
        && !(baseName == ".pgrun")
        && !(baseName == "redesign")
        && !(lib.hasSuffix ".zip" baseName);
  };
in
buildGoModule {
  pname = "mindful-social";
  version = "1.0.0-alpha";
  inherit src;

  # First-build flow: set this to lib.fakeHash, run `nix build`, then
  # replace with the hash the failed build prints.
  vendorHash = "sha256-J4chudhkxIu0JHDvkGdg0hWwJHZpGJlXgisCYF1EEdw=";

  subPackages = [ "cmd/server" ];

  # buildGoModule names the binary after the last path component of
  # subPackages, which would be "server". Rename to the project name so
  # systemd units and PATH lookups read naturally.
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
}
