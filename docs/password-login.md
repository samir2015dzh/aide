# Logging in with an account and password

> Status: implemented (2026-11-18, fork work order "登录改账号密码制"). A self-hosted
> relay only; a hosted center keeps its browser sign-in.

In one sentence: **the owner turns on an account and password on a trusted
device; a new phone, computer or agent then logs in with them alone**, with no
other device approving, no verification code to compare, no two digits to
type and no connection address to paste.

## 1. How it fits the trust model

Nothing that verifies changes. The trust anchor is still the root chain
([trusted-devices.md](trusted-devices.md) §3), and every credential is still
signed by a root key listed in the current root cert.

- **The password key** is one more root key in the root cert, with
  `platform = "password"` and the label 账号密码登录: a P-256 software key,
  like a Linux trusted device's passphrase key (`internal/devicekey`), except
  that its sealed file is kept on the relay instead of a disk.
- **Sealing.** `Argon2id(password, salt; t=3, 64 MiB, 4 lanes)` gives 64
  bytes: the first half is the **auth key**, the second the **seal key**. The
  password key's PKCS#8 DER is sealed with AES-256-GCM under the seal key,
  with the owner id and the key's public key as additional data
  (`internal/passlogin`).
- **What the relay keeps** (`relay.v1.PasswordLogin`, in its meta table):
  the account, the KDF's salt and costs, `SHA-256(auth key)` as the
  verifier, the nonce, the sealed key and the public key. It never sees the
  password or the seal key, so it can't open the key.
- **Logging in** (`StartLogin`, then `Login`): the client derives both keys,
  proves the auth key, gets the sealed key, opens it with the seal key,
  checks that the root chain verifies under the owner it names and lists
  that key, and then signs with it exactly what a trusted device would have
  approved:
  - a **device** (phone, computer): the next root cert adding its own root
    key, its device cert and, if it runs agents, its own machine's cert. It
    sends the chain with `Login{root_chain}`; the relay takes only one cert
    more, signed by the password key, that keeps every key and adds some.
  - an **agent** (`aide auth login --account`): its pairing request as
    usual, then `Login{pairing_code, credential}` with the grant an approval
    with the defaults gives (every machine, project and coding agent, its
    own tasks, until revoked).
- **No digits.** They exist because a center could route a join to an owner
  it controls ([trusted-devices.md](trusted-devices.md) §4.2). Here the key
  opens only with the person's password and is sealed to the owner's id, so
  a relay that hands out another owner's key gets nothing opened.
- **Nothing is signed on the new device with its own key.** Its root key is
  made (no confirmation) but only listed; so logging in asks for no Touch
  ID, Windows Hello or screen lock. Approving something later on that device
  still does, as on any trusted device.

## 2. Turning it on, changing it, turning it off

On a trusted device, Settings › Account › 账号密码登录 (`SetLoginPassword`),
one Touch ID / Windows Hello:

- **On**: a new password key in the next root cert, and its sealed copy on
  the relay (`SetPasswordLogin`). Passwords have at least 8 characters.
- **Change**: the next root cert drops the old password key **and every root
  key a cert signed by it added** (the devices that logged in with it), and
  adds a new password key. As when a device is removed, what those devices
  signed is re-issued by this one, their own machines are revoked, and the
  agents whose grants the old key signed are revoked. Their device certs
  were signed by the old key, so the relay cuts them off at once. A device
  that logged in with the password can't change it (it would sign itself
  out); a device that joined by approval or set the owner up can.
- **Off**: the same without a new key; the relay forgets the login.
- **Status** (`GetLoginPassword`): the account, the server to type, and the
  devices that logged in with the current password.

Devices and agents paired by approval are untouched by all three; the owner
id, the relay id and the relay's data stay as they are.

## 3. The server to type

No build names a server, and nothing falls back to the official center
(owner's ruling of 2026-11-19: builds are given to others, so none may
carry anyone's server). The login pages ask for the server with the
account and password, and Continue waits for all three; any address works
there (`relay.example.com`, with or without `https://`, or a connection
address, whose owner part is not needed). The CLI takes it as its argument,
else `$HERMESH_SERVER`; with neither it stops (`which server?`).

## 4. Guessing

- Per address: 20 login calls a minute (`loginsPerMinute`, counted across a
  cluster).
- Per account: after 5 wrong passwords in a row the account is locked for a
  minute, doubling with each further run, up to an hour
  (`lockAfter`, `lockFor`, `lockMax`). The count is the relay process's; a
  restart resets it.
- An unknown account gets made-up KDF parameters, the same each time, so
  answers don't tell which accounts exist; the error is the same
  ("wrong account or password").
- Nothing logs the password, the auth key or the seal key; the relay logs
  refusals and logins with the caller's address only.

## 5. What it costs

The promise of [trusted-devices.md](trusted-devices.md) was that no
exportable master secret exists. With this on, one does: the password.

| Attack | Result |
|---|---|
| Someone who knows the password | Is a trusted device of the owner: that is the feature. Change the password (all its logins go), or turn it off |
| A stolen copy of the relay's data | An offline guess costs one Argon2id (64 MiB, 3 passes) per try: a strong password holds, a weak one falls. This is the new exposure; the login page and settings ask for 8 characters at least |
| A relay that lies | Can't open the key or forge a login without the password; can withhold the login (as it can withhold anything) |
| Guessing online | 5 tries, then a lock that doubles; 20 calls a minute per address |
| A device that logged in, stolen | As any trusted device: remove it, or change the password (which removes every device that logged in) |

## 6. Code

| Area | What |
|---|---|
| `proto/hermesh/relay/v1` | `StartLogin`, `Login` (public); `SetPasswordLogin`, `GetPasswordLogin` (owner); `PasswordLogin`, `PasswordKdf` |
| `proto/hermesh/local/v1` | `PasswordLogin`, `SetLoginPassword`, `GetLoginPassword` |
| `internal/passlogin` | KDF, sealing, the client's login (`Login`, `Session.Join`, `Session.Complete`) |
| `pkg/relay/password.go` | The relay's side, its lockout; `completePairing` shared with `CompletePairing` |
| `internal/owner/password.go` | A device logging in; turning on, changing, turning off |
| `internal/cli/password.go` | `aide auth login --account` |
| `internal/connaddr` | `Server`, `ErrNoServer` |
| Desktop | First run: 账号密码登录 first (`firstrun/SignIn.tsx`); Settings › Account › 账号密码登录 (`settings/Account.tsx`); the password key is not listed among the trusted devices |
| Android | Onboarding: account and password first, the connection address and claim code as other ways (`ui/Onboarding.kt`) |
| Tests | `e2e/password_login_test.go` `TestPasswordLogin` (phone core, second computer, agent; change, old password refused, off; the approved agent's `auth status` unchanged); `pkg/relay` `TestPasswordLoginOnTheRelay`; `internal/passlogin`; `internal/connaddr` `TestServer` |
