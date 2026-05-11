# Non-flake entrypoint. Lets channels users build the package with
# `nix-build` and import the NixOS module without enabling flakes.
#
# Build the binary:
#     nix-build
#
# Use the NixOS module (from a configuration.nix):
#     imports = [ "${builtins.fetchTarball { url = "https://github.com/TuotHash/mindful-social/archive/<rev>.tar.gz"; sha256 = "..."; }}/nix/module.nix" ];
#     services.mindful-social.enable = true;
#     services.mindful-social.publicBaseURL = "https://mindful.example.org";
{ pkgs ? import <nixpkgs> {} }:

rec {
  mindful-social = pkgs.callPackage ./nix/package.nix {};

  # Convenience alias so `nix-build` (which looks for the top-level
  # attribute or the whole expression) hands back the binary.
  default = mindful-social;

  # Path to the NixOS module. Use as:
  #     imports = [ (import (fetchTarball ...) {}).nixosModule ];
  nixosModule = ./nix/module.nix;
}
