# The Android app

> Status: first build (2026-11-16, WO #1); notifications through the app's
> own connection, kept in the background by a foreground service
> (2026-11-17, §4.1; Xiaomi's Mi Push was given up the same day and removed
> on 2026-11-18, §4.2). A Kotlin + Jetpack Compose app for Android 11 and later, with
> the iPhone app's core features; the rest is listed in §8.

The Android phone is a **trusted device** of its owner
([trusted-devices.md](trusted-devices.md)), as the iPhone is
([iphone-app.md](iphone-app.md)): it holds a root key, approves and signs,
and sees every machine's sessions. It is never a machine.

## 1. What it does

| Place | What it shows |
|---|---|
| Logging in | The server, the owner's account and password ([accounts.md](accounts.md)): nothing to approve or compare. Or a new server, with the setup code it printed and the account and password it will keep |
| Sessions | Every machine's sessions by state (Needs You, In Progress, Recent), each with who sent the work (the agent's grant name, "Her", or "You", and the name it started under), the coding agent, project, machine and state; older pages on request |
| A session | Its conversation, live: the owner's messages, the agent's replies (a reply being written shows as it is written), tool calls folded into one line a run, opening to each call and its result, questions and how they were resolved. At the bottom the agent's question to answer (options, multi-select, free text, secrets hidden) or leave to the agent, or Interrupt while it works |
| Approvals | Requests to connect: machines (named here), agents (every project and coding agent, for 7, 30 or 90 days) and trusted devices, with the verification code to compare; Dismiss hides one here. A pairing code typed, or a `hermesh://pair?code=…` link, looks one up |
| Machines | Each machine: online, unreachable or updating, platform, version, sessions running, its coding agents (version, signed in), Her; Check Agents Again. The trusted devices. This phone: server, connection address, where its root key is kept (StrongBox, TEE), the core's version, the connection (connected, reconnecting, disconnected), the switch for staying connected in the background, notifications (§4) |
| Notifications | Through the app's own connection (§4.1): a new session, a question, a failure, a request to connect, Her; a tap opens what it is about |
| Her | The machines that run Her (§5) |

Signing: every batch the core asks the phone to sign is shown first, as the
shared core words it (`internal/preview`, the cards the Linux and Windows
signers show), unless the person confirmed exactly that in the app already
(they compared the verification code approving a pairing, or it is their own
join or setup); then BiometricPrompt confirms it is them. The two digits to
type on a device or machine just approved show after signing.

Words are English, or Chinese on a phone set to Chinese (`ui/Words.kt`).

## 2. How it is built

```
apps/android/
├── app/src/main/java/io/aide/android/
│   ├── HermeshApp.kt, MainActivity.kt   the core started with the process; the window, BiometricPrompt
│   ├── api/       Connect client (JSON codec) for LocalService, its messages
│   ├── model/     AppModel (state, actions, Watch, lifecycle), World, Transcript, links, answers
│   ├── signer/    RootKeys (Keystore), AppSigner (the core's Signer), cards and expectations
│   ├── her/       Her's protocol over the core's HerOpen/HerCall/HerSend, the panel's state
│   ├── connection/ the foreground service that keeps the connection in the background, its switch (§4.1)
│   ├── push/      notifications: what the Watch brings as notices (LiveNotices), showing them
│   └── ui/        Compose screens
├── app/src/test/  JVM unit tests
├── scripts/build-core.sh   gomobile bind → build/core/hermeshcore.aar
└── scripts/build.sh        the core, the app and its tests → build/Hermesh-<version>-debug.apk
cmd/hermesh-mobile/android/  the Go package bound for Android (package hermeshcore)
```

- **The same core as the iPhone's.** `cmd/hermesh-mobile/android` wraps
  `internal/mobile` (the owner role without a local machine) with
  gomobile-friendly calls: `Start(dataDir)` (port and token), `Resume`,
  `Pause`, `NetworkChanged`, `PathChanged`, as the iPhone's C library has
  them, `KeyID` and `RunSigner`. Nothing in it is Android's alone: the
  iPhone app keeps building `cmd/hermesh-mobile` as before, and both share
  `internal/mobile` (and its log, `mobile.Logger`). gomobile and gobind are
  `go.mod` tools, so their version is the module's.
- **LocalService on loopback.** As on the iPhone: `127.0.0.1` at a random
  port, a bearer token made at each launch. The app speaks Connect with the
  JSON codec over OkHttp (`api/Connect.kt`: unary calls, server streams'
  envelopes); `network_security_config.xml` allows plain HTTP to
  `127.0.0.1` alone. The core's own connections (the relay, the machines)
  are Go's.
- **In front and behind.** `ProcessLifecycleOwner`: in front the core runs
  (`Resume`) and the app follows the owner's world through one `Watch` and a
  session's events through `StreamSessionEvents` (follow, drafts); behind,
  the connection service keeps the core and the `Watch` running (§4.1), or,
  with it switched off, the core pauses (`Pause`: no relay, no signer), as
  the iPhone does. A change of network (`ConnectivityManager`) connects
  again at once.
- **Data.** `filesDir/hermesh` (the owner role's), no backup or device
  transfer (`data_extraction_rules.xml`): the data names a root key that
  lives in this phone's Keystore alone.

## 3. The signer

The design follows the Mac's Secure Enclave signer and the Windows TPM
signer (`internal/signer`): the core's `signer.Run` answers signing
requests, the app implements its `Keys` and `Owner` through the binding's
`Signer` interface (`signer/AppSigner.kt`).

- **Keys** (`signer/RootKeys.kt`): one P-256 key per profile the owner role
  keeps, made in the Android Keystore, in **StrongBox** when the phone has
  one, else in the **TEE**; the private key never leaves it. Each is bound to
  the person: `setUserAuthenticationRequired` with
  `AUTH_BIOMETRIC_STRONG | AUTH_DEVICE_CREDENTIAL` for 10 seconds after a
  confirmation (a batch of re-issued grants takes that long in StrongBox;
  a batch that outlasts it asks again), usable only while the phone is
  unlocked, and kept when a fingerprint is added (as Face ID's
  `userPresence` keeps the iPhone's). The core names keys by `KeyID` (a
  fingerprint of the public key), which the app asks the binding for.
- **Creating** a key needs no confirmation; **deleting** one happens when
  the core asks (the phone left that owner); **signing** needs the cards
  approved, then BiometricPrompt. A phone without a screen lock cannot make
  such keys, and says so.
- **Posture**: as on the iPhone (Secure Enclave, `userPresence`) and Windows
  (TPM, Windows Hello): the key cannot be exported, every use needs the
  person, what is signed is shown from the signed bytes by the shared core.

## 4. Notifications

The owner's phone is a Xiaomi 15 Pro on HyperOS's Chinese ROM, without
Google Play services, so FCM cannot reach it; Xiaomi's Mi Push now
takes only enterprise developers, which the owner decided against
(2026-11-17). So the app hears of what needs the owner the way the desktop
app does: through its own connection to the relay, kept up in the
background by a foreground service.

### 4.1 The app's own connection (in use)

1. **What is notified.** The core's `Watch` brings every change of the
   owner's world (sessions, machines and their Her, requests to connect).
   `push/LiveNotices.kt` compares each with what it saw before, by the
   desktop's rules (`packages/core` `notices.ts`, `apps/desktop` `notify.ts`),
   and says what is new as the notices a machine seals for a phone
   (`session.v1.Notice`): a session started by someone other than the owner
   after the app first looked; a question (also one still waiting when the
   app starts); a failure (not an interruption); an assistant's newer
   message in Her, unless muted; every request to connect. The kinds are
   the iPhone's defaults (`noticeKinds`; finished turns are not notified).
   They are shown by `Push.notify` (`Notices.kt` `show`):
   one notification a session (and one for requests to connect, one per
   assistant), a tap opening the session, Approvals or Her; on a locked
   screen only the generic "Something needs you" (`VISIBILITY_PRIVATE`).
   What is no longer pending is taken back: a question answered here or
   elsewhere, the last request to connect gone, Her read everywhere.
   Nothing new reaches the relay or the machines: no registration, no push
   service, nothing for Xiaomi.
2. **Staying connected.** Once the phone is joined to an owner, and while
   **Machines › This phone › Stay connected in the background** is on (the
   default), the app starts `ConnectionService`, a foreground service
   (`foregroundServiceType="specialUse"`: none of the other types fits a
   connection to one's own server, and `dataSync` is limited to 6 hours a
   day from Android 15) with a quiet ongoing notification ("Hermesh is
   connected", its channel of low importance). While it runs, the core
   stays resumed and the `Watch` goes on when the app goes behind, so
   notices come while the app is in the background or the screen is locked.
   Switched off, the service stops and the app is as before: the core
   pauses behind, and notices come only while the app is open.
3. **Reconnecting.** The core already tries the relay again with
   exponential backoff (1 s, doubling, at most 10 s) and at once when the
   network changes (Wi-Fi to cellular and back: `NetworkChanged`,
   `PathChanged`). The app shows the connection as **connected**,
   **reconnecting** (a network but no relay) or **disconnected** (no
   network) on Machines › This phone, under each page's title, and in the
   service's notification. Back online, the `Watch` brings the world again:
   a question that waits, a request to connect, a session started meanwhile
   is notified then; a failure, or a Her message already read, that came
   and went while disconnected is not.
4. **When the system ends it.** `START_STICKY`: the system starts the
   service again when it reclaimed it, when Android allows it then (12+
   may refuse a start from the background; the service then stops). Nothing
   goes around the system's limits: no wake locks, no second process, no
   alarms. On HyperOS, swiping Hermesh away from Recents or a force stop
   ends it; notices then wait until the app is opened. To keep it alive
   longer the app asks once (skippable, not again) to set **Battery saver:
   No restrictions** and **Autostart: on** (§4.2), with a button to the
   app's settings page; the same button stays on Machines › This phone.
5. **Permissions.** `POST_NOTIFICATIONS` (asked on Android 13+, now in
   every build), `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_SPECIAL_USE`.

The limits, accepted by the owner: it is not a push service. A service
HyperOS killed hears nothing until the app runs again, and the phone keeps
a connection open (and the core's `Watch`, compared each second) while it
runs.

### 4.2 HyperOS settings

Mi Push, built in the second round and given up on 2026-11-17 (Xiaomi's
developer platform registers enterprises only), was removed on 2026-11-18
with the fork's subtraction (C4): the relay's sender, the app's receiver
and its notice key are gone; the relay refuses a registration for it.


HyperOS stops apps in the background and keeps them from starting
themselves. For notices while Hermesh is closed (these also keep §4.1's
connection service alive longer, and are what its guide asks for):

- **Notifications**: Settings › Notifications & status bar › App
  notifications › Hermesh: allowed, with lock screen and banners
  (悬浮通知、锁屏通知).
- **Autostart** (自启动): Settings › Apps › Manage apps › Hermesh ›
  Autostart: on, so that the system may start the connection service again.
- **Battery** (省电策略): Settings › Apps › Manage apps › Hermesh › Battery
  saver: **No restrictions** (无限制).
- Optionally lock Hermesh in Recents (pull its card down, 锁定), so
  clearing Recents spares it.

### 4.3 Not yet

Answering a question from the notification (the iPhone's actions), choosing
which kinds come (fixed as the iPhone's defaults), FCM for phones with
Google Play services (a `Pusher` beside APNs, with the owner's own
Firebase project), notices for a phone whose app is not running at all.

## 5. Her

The core already carries everything Her needs (`HerOpen`, `HerCall`,
`HerSend`, docs/her-server.md), so the app has a minimal Her panel without
any change to it: the machines that run Her (any platform her runs on), and
on each, her default assistant's

- **conversation**: its snapshot and events (`bots/open`), the owner's
  messages and what Her sent (`send_to_user`), a task that failed; sending
  words (`HerSend`, kept on the phone until the machine is back); her tool
  calls that wait for the owner, allowed once or denied (`session/op`);
- **routines**, read-only (from `bots/subscribe`);
- **memory**, read-only (`bots/memory`).

Signed Her changes (turning Her on or off, connectors) go through the signer
as every other approval does. Not there: pictures and files, quoting a
message, other assistants, secure cards, sub-agents' progress, settings
(§8).

## 6. Building and installing

Needs Go (the module's version), a JDK 17 or later, and the Android SDK with
`platforms;android-36`, `build-tools;35.0.0` and an NDK:

```
sdkmanager --sdk_root="$ANDROID_HOME" "platform-tools" "platforms;android-36" "build-tools;35.0.0" "ndk;27.3.13750724"
ANDROID_HOME=<sdk> apps/android/scripts/build.sh          # → apps/android/build/Hermesh-0.0.0-dev-debug.apk
adb install -r apps/android/build/Hermesh-0.0.0-dev-debug.apk
```

`build.sh` runs Gradle (`./gradlew`, its wrapper fetches Gradle 8.14.3),
whose `preBuild` runs `build-core.sh` (gomobile bind for `android/arm64`
and `android/amd64`, skipped when nothing in Go changed; `FORCE=1` builds
again), then `assembleDebug` and the unit tests (`SKIP_TESTS=1` skips them).
`VERSION=0.1.2` stamps the version. The APK is debug-signed (application id
`io.aide.android.debug`); outputs and caches stay out of git
(`apps/android/.gitignore`).

## 7. Tests

- `apps/android/scripts/build.sh` (or `./gradlew :app:testDebugUnitTest`):
  JVM unit tests of the Connect client against a server playing the core
  (unary, streams, errors, what requests carry), the protojson models (as
  Go's protojson writes them), the world (resets, merges, buckets, who sent
  a session), transcripts, answers, links, the signer's cards and
  expectations, Her's protocol, and notices: each kind as shown in English
  and Chinese, what a tap opens; the
  notices the `Watch` brings (`LiveNoticesTest`: Watch messages as the core
  writes them in, the notices and what is taken back out: the first look,
  new sessions not the owner's, failures but not interruptions, questions,
  Her muted or read, requests to connect, one looked up here) and the
  connection as shown.
- `go test ./cmd/hermesh-mobile/...`: the binding (start, signer adapters).
- `go test ./e2e -run TestAndroidCore`: the bound core end to end, driven
  as the app drives it: joins an owner Mac as a trusted device (its root key
  made through the app's signer), approves an agent, follows its session
  live, gets its question, answers it, and the session ends with the
  answer. Beside it the
  test follows the core's `Watch` as the app does (the JSON codec): the
  request to connect and the question show there before the phone answers;
  then the owner starts a session of their own, Muse one that fails, and
  one that asks while the relay is stopped: the `Watch` shows the relay
  gone, then back (the core reconnecting by itself), then the question.
  With `HERMESH_ANDROID_WATCH_RECORD=<file>` it writes that `Watch` out,
  for `LiveNoticesReplayTest`, which plays it through `World` and
  `LiveNotices` as `AppModel` does (`app/src/test/resources/watch-recorded.jsonl`
  is one such recording; the env var makes it play a fresh one): one
  notice for the request to connect, Muse's three sessions but not the
  owner's, both questions (the second only once reconnected), the failure,
  and what is taken back.
- `go test ./e2e -run TestPushNotices` also checks that the relay refuses
  a registration for a push service it doesn't know (Mi Push's, since its
  removal).
- Not yet: UI tests on a device or emulator, so the connection service
  itself (started, kept, restarted, switched off) is checked on the phone
  (§4.1).

## 8. Not yet (compared with the iPhone app)

- Notifications: answering from them, choosing their kinds (§4.3).
- The public demo; Overview (work counts, charts); search; grouping, sorting
  and archived sessions in the list; archive, rename, delete a session; the
  diff and session info; Markdown, code colours, math and diagrams in
  replies (shown as plain text); a tool call's whole output
  (`ReadToolOutput`); conversations kept on the phone (`world.pb`).
- Grants: renew, change scope, rename, disconnect, pause; an agent approved
  for some projects or coding agents only (always all, for 7/30/90 days).
- Machines: rename, remove, pause; coding agents' defaults and projects;
  removing a trusted device; profiles (more than one owner); sign out,
  leave, erase everything.
- Joining or setting up through the official service (sign-in in a
  browser); QR codes; a hosted center's `.well-known` connect URL.
- Her beyond §5.
- Words in Android resources (they are in code, English and Chinese).
