// The version a Windows build may carry.
//
// Squirrel's launcher (%LOCALAPPDATA%\AideApp\Aide.exe, electron-winstaller's
// StubExecutable) starts the newest app-<version> folder beside it, but only
// one whose semver is above 0.0.0: with none, it builds a string from NULL and
// crashes (0xc0000005 in std::wstring::assign, its own directory read fine).
// A pre-release of 0.0.0 (0.0.0-local, 0.0.0-dev) sorts below 0.0.0, so an
// app installed at such a version never starts from its shortcut, its taskbar
// pin or an aide: link, though Update.exe --processStart, which picks the
// folder itself, does. Such versions are refused before anything is built.

const semver = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

/** Why version can't be a Windows build's, or "" when it can. */
export function windowsVersionProblem(version) {
  const m = semver.exec(String(version ?? ""));
  if (!m) return `${JSON.stringify(version)} is not a semantic version (major.minor.patch, as 0.2.3 or 0.0.1-local)`;
  if (m.slice(1, 4).every((n) => n === "0")) {
    return `${version} is not above 0.0.0, so Squirrel's launcher finds no app to start and crashes (its shortcut and aide: links with it): use a version above 0.0.0, as 0.0.1-local for a build of one's own`;
  }
  return "";
}
