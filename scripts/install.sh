#!/bin/sh
# Aide installer. Downloads release binaries from GitHub Releases,
# verifies them against a signed checksum list, installs them under
# ~/.local/bin and (for machines) logs the machine in to your server with your
# account and password (docs/accounts.md).
#
#   Add a machine:   curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- <server>
#                    (without one: $AIDE_SERVER; there is no default server). It asks for the
#                    account and password, or takes them from $AIDE_ACCOUNT and $AIDE_PASSWORD.
#   Agent CLI only:  curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- --cli
#   As root:         … | sh -s -- --user dev   (installs for that user, creating it if needed; without it, asks, and root is the default)
#
# Environment: AIDE_VERSION (default latest), AIDE_BIN_DIR (default ~/.local/bin); the AIDE_ names too
set -eu

REPO="samir2015dzh/aide"
AIDE_VERSION="${AIDE_VERSION:-}"
AIDE_BIN_DIR="${AIDE_BIN_DIR:-}"
AIDE_DOWNLOAD_BASE="${AIDE_DOWNLOAD_BASE:-}"
AIDE_DATA_DIR="${AIDE_DATA_DIR:-}"
VERSION="${AIDE_VERSION:-latest}"
BIN_DIR="${AIDE_BIN_DIR:-$HOME/.local/bin}"

# Release signing public key (ECDSA P-256). Replaced at release time.
PUBKEY='__AIDE_RELEASE_PUBKEY__'

die() { echo "aide install: $*" >&2; exit 1; }

MODE=machine
ADDRESS=""
NAME=""
FOR_USER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --cli) MODE=cli ;;
    --name) shift; NAME="${1:-}" ;;
    --user) shift; FOR_USER="${1:-}" ;;
    -*) die "unknown option $1" ;;
    *) ADDRESS="$1" ;;
  esac
  shift
done

case "$(uname -s)" in
  Darwin) die "this Aide has no macOS build: install it on Linux, or the Windows app" ;;
  Linux) OS=linux ;;
  *) die "unsupported OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac
# A terminal to ask on: under `curl … | sh` stdin is this script.
TTY=""
if (: </dev/tty) 2>/dev/null; then TTY=/dev/tty; fi

CYAN="" BOLD="" DIM="" RESET=""
if [ -n "$TTY" ] && [ -t 1 ] && [ "${TERM:-dumb}" != dumb ] && [ -z "${NO_COLOR+x}" ]; then
  CYAN="$(printf '\033[36m')" BOLD="$(printf '\033[1m')"
  DIM="$(printf '\033[2m')" RESET="$(printf '\033[0m')"
fi
printf '\n%s  /\\  /\\%s\n%s /  \\/  \\   H E R M E S H%s\n' "$CYAN" "$RESET" "$CYAN" "$RESET"
printf '%s             Your coding agents, connected.%s\n\n' "$DIM" "$RESET"

# Aide runs as the user it is installed for. As root, the installer says
# so and installs for root, or for another user you name.
AS=""
if [ "$(id -u)" = 0 ] && [ -z "$FOR_USER" ] && [ -n "$TTY" ]; then
  echo "You are root: Aide will be installed for root."
  printf 'Press Enter to continue, or type another user to install for (q quits): '
  read -r FOR_USER <"$TTY" || true
  case "$FOR_USER" in q | Q) echo "Nothing was installed."; exit 0 ;; esac
fi
if [ "$(id -u)" = 0 ] && [ "${FOR_USER:-root}" = root ]; then
  # For root itself, whose service starts at boot with nobody logged in.
  # USER may be unset (cloud-init, docker exec) or still another's (su).
  FOR_USER=""
  export USER=root LOGNAME=root
  if command -v loginctl >/dev/null && loginctl enable-linger root 2>/dev/null; then
    echo "✓ Installing for root, with lingering on"
  else
    echo "✓ Installing for root"
  fi
elif [ "$(id -u)" = 0 ]; then
  if ! id "$FOR_USER" >/dev/null 2>&1; then
    if [ -n "$TTY" ]; then
      printf '“%s” doesn'"'"'t exist. Create it? [Y/n] ' "$FOR_USER"
      read -r yes <"$TTY" || true
      case "$yes" in n* | N*) die "nothing was installed" ;; esac
    fi
    useradd -m -s /bin/bash "$FOR_USER" || die "could not create the user $FOR_USER"
    echo "✓ Created $FOR_USER"
  fi
  uid="$(id -u "$FOR_USER")"
  home="$(getent passwd "$FOR_USER" | cut -d: -f6)"
  if loginctl enable-linger "$FOR_USER" 2>/dev/null; then
    echo "✓ Installing for $FOR_USER, with lingering on"
  else
    echo "✓ Installing for $FOR_USER"
  fi
  BIN_DIR="${AIDE_BIN_DIR:-$home/.local/bin}"
  AS="runuser -u $FOR_USER -- env HOME=$home USER=$FOR_USER LOGNAME=$FOR_USER XDG_RUNTIME_DIR=/run/user/$uid"
elif [ -n "$FOR_USER" ]; then
  die "--user is for root; you are installing for yourself already"
fi

command -v curl >/dev/null || die "curl is required"
command -v openssl >/dev/null || die "openssl is required to verify the download"

if [ -n "${AIDE_DOWNLOAD_BASE:-}" ]; then
  BASE="$AIDE_DOWNLOAD_BASE"
elif [ "$VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Keep the ordinary data path as a stable entry point. A first install
# may link it to another disk; commands and future updates then find the
# same data without shell exports or a second configuration system.
prepare_data_dir() {
  target_home="$HOME"
  target_uid="$(id -u)"
  data_base="${XDG_DATA_HOME:-$HOME/.local/share}"
  if [ -n "$AS" ]; then
    target_home="$home"
    target_uid="$uid"
    # Match the environment runuser passes to aided.
    data_base="${XDG_DATA_HOME:-$home/.local/share}"
  fi
  DATA_ENTRY="$data_base/aide"
  DATA_ENTRY="${AIDE_DATA_DIR:-$DATA_ENTRY}"

  printf '\n%s  Where should Aide keep its data?%s\n' "$BOLD" "$RESET"
  echo '  Sessions, logs and worktrees will live here.'
  echo
  if [ -L "$DATA_ENTRY" ] && [ ! -d "$DATA_ENTRY" ]; then
    die "data directory link is unavailable: $DATA_ENTRY. Mount its disk before installing again."
  fi
  if [ -e "$DATA_ENTRY" ]; then
    [ -d "$DATA_ENTRY" ] || die "$DATA_ENTRY is not a directory"
    printf '  Existing data: %s\n' "$DATA_ENTRY"
    # shellcheck disable=SC2016 # Expanded by the target user's shell.
    printf '  Stored at:    %s\n' "$( $AS sh -c 'cd "$1" && pwd -P' sh "$DATA_ENTRY" )"
    show_space "$DATA_ENTRY"
    echo '  Keeping this location. Moving existing data is a separate migration.'
    return
  fi

  # Only DSM gets storage-volume suggestions. Ordinary Linux and macOS
  # keep their platform default. Never mistake an unmounted directory,
  # an NFS share or tmpfs for a NAS data volume.
  : > "$TMP/volumes"
  recommended="$DATA_ENTRY"
  best_free=0
  home_on_volume=false
  if [ "$OS" = linux ] && [ -f /etc.defaults/VERSION ] && [ -z "${AIDE_DATA_DIR:-}" ]; then
    awk '$2 ~ /^\/volume[0-9]+$/ && ($3 == "btrfs" || $3 == "ext4") && $4 ~ /(^|,)rw(,|$)/ {print $2}' /proc/mounts > "$TMP/mounts"
    while IFS= read -r volume; do
      $AS test -w "$volume" || continue
      available="$(free_kb "$volume")"
      [ "$available" -ge 2097152 ] || continue
      case "$DATA_ENTRY" in "$volume"/*) home_on_volume=true ;; esac
      destination="$volume/@aide-$target_uid"
      # A pre-existing destination might belong to another installation.
      if [ -e "$destination" ] || [ -L "$destination" ]; then continue; fi
      printf '%s\n' "$destination" >> "$TMP/volumes"
      if [ "$available" -gt "$best_free" ]; then
        best_free="$available"
        recommended="$destination"
      fi
    done < "$TMP/mounts"
    # A home already on a data volume needs no special placement.
    if [ "$home_on_volume" = true ]; then recommended="$DATA_ENTRY"; fi
  fi

  printf '  %sDefault: %s%s\n' "$CYAN" "$recommended" "$RESET"
  show_space "$recommended"
  if [ -s "$TMP/volumes" ]; then
    echo
    echo '  Synology storage volumes:'
    number=0
    while IFS= read -r destination; do
      number=$((number + 1))
      printf '  %s) %s\n' "$number" "$destination"
      show_space "$destination"
    done < "$TMP/volumes"
  fi
  echo
  echo '  Enter accepts the default. Or paste an absolute path (~/ works too).'
  [ -s "$TMP/volumes" ] && echo '  You can also type a volume number.'
  echo '  Spaces are welcome; no quotes needed. Type q to quit.'

  while :; do
    choice=""
    if [ -n "$TTY" ]; then
      printf '\n%s  Data location [%s]: %s' "$BOLD" "$recommended" "$RESET"
      IFS= read -r choice < "$TTY" || die "input closed; nothing was installed"
    else
      echo '  No terminal: using the default location.'
    fi
    case "$choice" in
      q | Q) echo 'Nothing was installed.'; exit 0 ;;
      '') selected="$recommended" ;;
      *[!0-9]*) selected="$choice" ;;
      *) selected="$(awk -v number="$choice" 'NR == number {print; exit}' "$TMP/volumes")" ;;
    esac
    # shellcheck disable=SC2088 # Match a literal tilde entered at the prompt.
    case "$selected" in '~') selected="$target_home" ;; '~/'*) selected="$target_home/${selected#\~/}" ;; esac
    while [ "$selected" != / ] && [ "${selected%/}" != "$selected" ]; do selected="${selected%/}"; done
    problem=""
    case "$selected" in /*) ;; *) problem='Please enter an absolute path or a listed volume number.' ;; esac
    case "$selected" in "$DATA_ENTRY"/*) problem='Choose a folder outside the default data directory, or press Enter to use the default.' ;; esac
    if printf '%s' "$selected" | LC_ALL=C grep '[[:cntrl:]]' >/dev/null; then
      problem='Please use a path without control characters.'
    fi
    if [ -z "$problem" ] && { [ -e "$selected" ] || [ -L "$selected" ]; }; then
      if ! $AS test -d "$selected" || ! $AS test -w "$selected"; then
        problem='This location is not a writable directory.'
      elif [ -n "$( $AS ls -A "$selected" )" ]; then
        problem='This folder is not empty. Choose a new or empty folder for Aide.'
      fi
    fi
    if [ -z "$problem" ]; then
      available="$(free_kb "$selected")"
      if [ "$available" -lt 2097152 ]; then
        echo '  Less than 2 GiB is available here (or space could not be checked).'
        echo '  Worktrees and session history may run out of space. A larger disk is recommended.'
        if [ -n "$TTY" ]; then
          printf '  Use this location anyway? [y/N] '
          IFS= read -r answer < "$TTY" || die 'input closed; nothing was installed'
          case "$answer" in y | Y | yes | YES) ;; *) continue ;; esac
        fi
      fi
    fi
    if [ -z "$problem" ] && ! (umask 077; $AS mkdir -p "$selected"); then
      problem='Could not create this folder as the installation user. Choose a writable location.'
    fi
    if [ -n "$problem" ]; then
      printf '\n  %s\n' "$problem"
      [ -n "$TTY" ] || die "set AIDE_DATA_DIR to a writable data directory"
      continue
    fi
    if [ "$selected" != "$DATA_ENTRY" ]; then
      # ln (without -f) never replaces an existing installation. The
      # dangling link also prevents writes if its data volume goes away.
      (umask 077; $AS mkdir -p "$(dirname "$DATA_ENTRY")")
      $AS ln -sT "$selected" "$DATA_ENTRY" 2>/dev/null || {
        # BSD ln has no -T; check first so it cannot put a link inside a directory.
        if [ -e "$DATA_ENTRY" ] || [ -L "$DATA_ENTRY" ]; then
          die "data entry already exists: $DATA_ENTRY; it was not replaced"
        fi
        $AS ln -s "$selected" "$DATA_ENTRY"
      }
    fi
    printf '\n%s  ✓ Data location: %s%s\n' "$CYAN" "$selected" "$RESET"
    echo '  Aide will remember this location for future commands and updates.'
    break
  done
}

# df needs an existing ancestor for a folder the installer will create.
free_kb() {
  probe="$1"
  while [ ! -e "$probe" ] && [ "$probe" != / ]; do probe="$(dirname "$probe")"; done
  df -Pk "$probe" 2>/dev/null | awk 'NR == 2 {print $4; found=1} END {if (!found) print 0}'
}
show_space() {
  free_kb "$1" | awk '{printf "     %.1f GiB available\n", $1 / 1048576}'
}

case "$MODE" in
  cli) BINS="aide" ;;
  *) BINS="aided aide" ;;
esac

echo "› downloading checksums"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt"
curl -fsSL "$BASE/checksums.txt.sig" -o "$TMP/checksums.txt.sig"
case "$PUBKEY" in
  __AIDE*) die "this installer has no release signing key; download a released install.sh" ;;
esac
printf '%s\n' "$PUBKEY" > "$TMP/pub.pem"
openssl dgst -sha256 -verify "$TMP/pub.pem" -signature "$TMP/checksums.txt.sig" "$TMP/checksums.txt" >/dev/null 2>&1 \
  || die "checksum signature verification FAILED; refusing to install"

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
# fetch downloads a release file into $TMP, checked against the signed list.
fetch() {
  echo "› downloading $1"
  curl -fsSL "$BASE/$1" -o "$TMP/$1"
  want="$(grep " $1\$" "$TMP/checksums.txt" | cut -d' ' -f1)"
  [ -n "$want" ] || die "$1 is not in the signed checksum list"
  [ "$(sha256 "$TMP/$1")" = "$want" ] || die "$1 checksum mismatch"
}
put() { # file, where: installed for the user Aide is for
  if [ -n "$AS" ]; then
    install -m 0755 -o "$FOR_USER" -g "$(id -gn "$FOR_USER")" "$1" "$2"
  else
    install -m 0755 "$1" "$2"
  fi
}

[ "$MODE" = cli ] || prepare_data_dir

if [ -n "$AS" ]; then
  $AS mkdir -p "$BIN_DIR"
else
  mkdir -p "$BIN_DIR"
fi
for b in $BINS; do
  fetch "$b-$OS-$ARCH"
  put "$TMP/$b-$OS-$ARCH" "$BIN_DIR/$b"
done
# Versions with Her put her beside aided, with her console, her computer
# use and her PDF tools (her-console/, her-computer/, her-pdf/). Aide no
# longer has Her: installing over one of them removes them, with what an
# update kept of them (internal/update).
if [ "$MODE" = machine ]; then
  for f in her her-console her-computer her-pdf; do
    rm -rf "${BIN_DIR:?}/$f" "$BIN_DIR/.$f.previous" "$BIN_DIR/.$f.new"
  done
fi
echo "✓ installed to $BIN_DIR"
if [ -z "$AS" ]; then
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) echo "note: $BIN_DIR is not on your PATH; add it with:"
       echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile && . ~/.profile" ;;
  esac
fi

if [ "$MODE" = cli ]; then
  echo "Next: aide auth login <your server> --account <your account>"
  exit 0
fi

set -- login
[ -n "$NAME" ] && set -- "$@" --name "$NAME"
[ -n "$ADDRESS" ] && set -- "$@" "$ADDRESS"
# The account, when given in the environment, goes along to aided login
# (runuser keeps no environment of its own); the password goes on its
# stdin, never on a command line.
ACCOUNT="${AIDE_ACCOUNT:-}"
PASSWORD="${AIDE_PASSWORD:-}"
[ -n "$ACCOUNT" ] && set -- "$@" --account "$ACCOUNT"
# The terminal goes to aided as its stdin, for what it asks.
if [ -n "$PASSWORD" ]; then
  printf '%s\n' "$PASSWORD" | env -u AIDE_PASSWORD $AS "$BIN_DIR/aided" "$@" --password-stdin
elif [ -n "$TTY" ]; then
  $AS "$BIN_DIR/aided" "$@" <"$TTY"
else
  $AS "$BIN_DIR/aided" "$@"
fi
# Installed by root: its commands, as the service, are that user's.
if [ -n "$AS" ]; then
  echo
  echo "Aide runs as $FOR_USER: run these as $FOR_USER (su - $FOR_USER)"
  if ! su - "$FOR_USER" -c 'command -v aided' >/dev/null 2>&1; then
    echo "  ($FOR_USER's PATH lacks $BIN_DIR: echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> $home/.profile)"
  fi
fi
