# Development

```bash
make generate   # Go, Swift and TypeScript code from proto
make lint test  # static checks and unit tests
make test-app   # the desktop app's types and unit tests
make e2e        # full user flows and malicious-relay tests with real binaries and scripted agents
make e2e-real   # the same flows with the real Claude Code and Codex on this machine (uses quota)
AIDE_DOCKER=1 go test ./e2e -run Linux   # a Linux machine (Docker)
AIDE_IPHONE_E2E=1 go test ./e2e -run TestIPhone -timeout 60m   # the iPhone app in the Simulator
make app        # build and sign dist/Aide.app (see below for a release build)
pnpm --dir apps/desktop dev   # the desktop app against the local aided (AIDE_SOCKET to choose one)
```

`make e2e` includes the desktop app's tests, which open the app on this Mac;
`go test ./e2e -skip 'TestDesktop$'` leaves them out.

## Releases

A release build of the desktop app needs, besides the Developer ID certificate in the keychain, the signer's Developer ID provisioning profile (App ID `com.aide.app.signer`; upstream's Mac app, whose signer this fork does not have, [accounts.md](accounts.md)) and an App Store Connect API key to notarize:

```bash
AIDE_SIGNER_PROFILE=~/.config/aide/signer.provisionprofile \
AIDE_NOTARY_KEY_PATH=~/.appstoreconnect/private_keys/AuthKey_<key id>.p8 \
AIDE_NOTARY_KEY_ID=<key id> AIDE_NOTARY_ISSUER=<issuer id> \
make app        # AIDE_ARCH=arm64 for an Apple silicon build only
```

The release workflow reads the same from the secrets `MACOS_SIGNER_PROFILE`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID` and `APPLE_API_ISSUER_ID`.

Releasing: every part of Aide has one version, and each version is one GitHub release holding every platform's files (the programs, the Mac app, the Windows app): every updater and the install command read `releases/latest/download/…`, which names one release. The iPhone app is not part of these releases: the same version goes to TestFlight from a Mac (`apps/ios/scripts/testflight.sh`, [iphone-app.md](iphone-app.md) §6).

1. **Notes.** Before tagging, the version's highlights go in `apps/desktop/src/locales/{en,zh-CN}/releases.json` (rename `unreleased` to the version, and leave an empty `unreleased`); without them the release workflow stops. Each line says where it applies when that is not everywhere ("On the iPhone…", "On Linux…"). The release's text starts with which file to install for what (`install` in the same folder's `updates.json`), then the highlights, then a link to every change since the last release.
2. **Tag.** Pushing a `v*` tag runs `release.yml`. It makes a draft release, the builds upload their files to it, and `scripts/release-check.sh` checks them all (every program in the signed checksum list, the Windows programs, the version, install.sh's and install.ps1's key, the Mac app's feeds naming this version). Only then is the draft published, as a **prerelease**: nothing installed updates to it, and the latest release stays as it was. The relay image of the version is on ghcr.io only. A failed run leaves a draft; fix it and run the workflow again, or delete the draft and tag a new version.
3. **Try it.** On a machine: `curl -fsSL https://github.com/samir2015dzh/aide/releases/download/<tag>/install.sh | AIDE_VERSION=<tag> sh` (on a machine logged in it updates it). On a Mac: the prerelease's DMG. Your own server: `ghcr.io/samir2015dzh/aide-relay:<tag>`.
4. **Promote.** `gh workflow run promote.yml -f tag=<tag>` checks the files again, makes the relay image latest on ghcr.io (nothing goes to Docker Hub, where `samir2015dzh` is upstream's), then makes the release the latest. Within hours every machine and Mac app updates to it; the workflow waits until `releases/latest` serves it.

**This fork** (`samir2015dzh/aide`) releases what its owner uses, under its own key and from its own releases:

- A `v*` tag builds the programs (`binaries`, with `her`, `her-windows` and `screen`), the Windows app for **x64**, the Android app (a debug build signed with the app's own key, `android` job; [android-app.md](android-app.md) §6.1) and the notes. The Windows app for arm64 and the relay image are built only when asked: run `release.yml` by hand on the tag (Actions › release › Run workflow, "Use workflow from" the tag) with `windows_arm64` or `relay_image` on. `release-check.sh` then checks the apps that release has (`RELEASE_APPS`, default both Windows apps; a tag release has `win32-x64`), and `promote.yml` takes the same list (`-f apps=…`, default `win32-x64`) and copies the relay image only with `-f relay_image=true`.
- The binaries are `aided-*`, `aide-*`, `aide-relay-*` (with copies under the names they had before the rename, for installations from before it to update: [aide-rename.md](aide-rename.md)), the Windows app is `Aide-windows-<arch>-setup.exe` and `.nupkg` with `Aide.ico`, the Android app `Aide-<version>-debug.apk`, and the relay image `ghcr.io/samir2015dzh/aide-relay`.
- The fork has no Mac app, macOS signer or macOS programs (removed on 2026-11-18, the fork's subtraction C3): what follows about `Aide-macos-*`, Developer ID signing and `TestDesktopUpdate` is upstream's, kept for reference.
- Releases are signed with the fork's key, not upstream's: `scripts/release-pubkey.pem` and `internal/update`'s `publicKey` are the fork's public key (`scripts/release-usekey.sh <public key PEM>` sets both; `TestPublicKeyIsTheReleaseKey` checks they agree), and the secret `RELEASE_SIGNING_KEY` is its private half. The release workflow's first step stops the run when the secret is missing or is not that key's private half, before anything is built.
- Everything that updates or installs reads the fork's releases (`https://github.com/samir2015dzh/aide/releases`): machines updating themselves (`internal/update`), the desktop app's feed and release links, `install.sh`/`install.ps1` (`REPO`), and the install commands the apps, aided and the relay show. A fork build therefore never updates itself to an upstream release, nor accepts one: upstream's are signed with upstream's key.

The first time, run `scripts/release-keygen.sh` to generate the release signing key (the private key goes in the GitHub secret `RELEASE_SIGNING_KEY`; commit the public key). Each release carries `Aide-macos-<arch>.dmg` for installation, and `Aide-macos-<arch>.zip` with `RELEASES-darwin-<arch>.json` for installed apps to update themselves (`arm64` and `x64`; [docs/desktop-app.md](desktop-app.md) §6.5–6.7). The app and DMG are Developer ID–signed, notarized and stapled. For Windows it carries `Aide-windows-<arch>-setup.exe` (unsigned) for installation, and `Aide-windows-<arch>.nupkg` with `RELEASES-win32-<arch>.json` and its `.sig`, signed with the release key, for installed apps to update themselves ([docs/windows.md](windows.md) §6.1–6.2). A whole update, with two signed builds made here and served locally, is checked by `AIDE_UPDATE_E2E=1 go test ./e2e -run TestDesktopUpdate -v -timeout 30m`. The binaries' signed `checksums.txt` also lists `version`, which installed machines read to update themselves ([machine-updates.md](machine-updates.md)); `AIDE_DOCKER=1 go test ./e2e -run TestLinuxUpdate -v` checks that on a Linux server in Docker. Releases before 2026-11-22 also listed her for Linux and Windows and her console; Aide no longer has Her, and installs and updates remove them.
