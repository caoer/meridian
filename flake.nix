{
  description = "meridian – llm-wiki lint engine";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = if self ? rev then self.rev else "dev";
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "meridian";
          inherit version;
          src = ./.;
          vendorHash = "sha256-XjABjyQ97W+TYhhx1Kw/LLQctzv7hg9U3RV3ghDsyPE=";
          ldflags = [
            "-s" "-w"
            "-X github.com/caoer/meridian/internal/version.version=${version}"
          ];
          # Tests that shell out to git need it in PATH.
          nativeCheckInputs = [ pkgs.git ];
          # TestWatcher_CreateEvent is flaky in the nix sandbox (fsnotify
          # reports create as modify on overlayfs). Full suite passes on
          # real filesystems and is gated by `go test ./...` in CI/local.
          checkFlags = [ "-skip" "TestWatcher_CreateEvent" ];
          meta = {
            description = "llm-wiki lint engine";
            mainProgram = "md";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [ go gopls gotools ];
        };
      }
    );
}
