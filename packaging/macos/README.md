# CodeDistill on macOS

Releases ship one universal `CodeDistill-<version>-macos.dmg` that runs on
Apple Silicon and Intel Macs. It is signed with the Nyx Software, Inc.
Developer ID and notarized by Apple, so it opens without Gatekeeper
warnings: mount the image, drag CodeDistill.app to Applications, double-click
it. The server starts and the browser opens http://localhost:8080. Data lives
under `~/Library/Application Support/CodeDistill/`.

## How it is built

1. The Linux Jenkins agent cross-compiles the server and the bundle's
   launcher (`cmd/codedistill-launcher`, a small Go program that replaced the
   old shell script, because notarization needs a Mach-O main executable)
   for arm64 and amd64, and stages two `CodeDistill.app` bundles with
   `scripts/build-dist.sh --target darwin --stage-only`.
2. The `macOS universal dmg` stage runs on the agent labelled `macos` (the
   Mac mini). `packaging/macos/build-mac.sh` lipo-merges the two bundles,
   builds the `.icns` from `CodeDistill-icon.png`, signs every binary and the
   bundle with the hardened runtime (`entitlements.plist`), builds the dmg
   with `hdiutil`, signs it, submits it to Apple's notary service and staples
   the ticket.
3. Signing credentials are company-wide Jenkins credentials shared with
   Hemera: `nyx-macos-cert-p12`, `nyx-macos-cert-password`,
   `nyx-notary-key-p8`, `nyx-notary-key-id`, `nyx-notary-issuer`. Without
   them the stage produces the same dmg unsigned.

The Mac agent clones nothing: the Linux side stashes `dist/mac/**`,
`packaging/macos/**`, `VERSION` and the icon, and the dmg is stashed back for
the Archive stage.

## Unsigned fallback

Run the pipeline with `SIGN_MACOS` unticked to get the previous behaviour:
per-architecture dmgs built entirely on Linux with libdmg-hfsplus
(`scripts/setup-dmg-tools.sh`). Those are unsigned; on first launch
right-click CodeDistill.app, choose Open, or approve it under System
Settings → Privacy & Security after the first blocked attempt.

## Building by hand on a Mac

    scripts/build-dist.sh --target darwin --stage-only
    P12=~/nyx-devid.p12 P12PASS=... NOTARY_KEY=~/Keys/AuthKey_XXXX.p8 \
      NOTARY_KEY_ID=XXXX NOTARY_ISSUER=... packaging/macos/build-mac.sh

Leave the signing variables out for an unsigned dmg.
