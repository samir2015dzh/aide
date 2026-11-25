#!/usr/bin/env python3
"""Split the end-to-end suite (./e2e) into shards balanced by measured time.

ci.yml runs the suite as parallel shards. Each shard asks this script for its
-run pattern; the e2e-ledger job then checks, from the shards' logs, that
every test the package lists ran exactly once, in the shard it was given to.

  go test ./e2e -list '^Test' > tests.txt
  e2e_shards.py run-pattern --of 4 --shard 1 tests.txt     # one shard's -run
  e2e_shards.py plan --of 4 tests.txt                      # every shard, with times
  e2e_shards.py ledger --of 4 tests.txt shard1.log ...     # the check, and new times

Times come from scripts/e2e_shard_times.json: seconds per top-level test, as
the shards' own logs reported them on the CI runner (Linux, race binaries).
The ledger prints a fresh copy of that file; refresh it when the shards drift
apart. A test the file does not know yet weighs the median of those it does,
so a new test lands somewhere and runs, before its time is known.

Shards are filled longest test first, each to the shard with least time so
far (ties to the lower shard): the same inputs give the same split everywhere.
"""

import argparse
import json
import re
import statistics
import sys
from pathlib import Path

TIMES = Path(__file__).with_name("e2e_shard_times.json")
DESKTOP_TIMES = Path(__file__).with_name("windows_desktop_times.json")
DESKTOP_TEST = Path(__file__).resolve().parent.parent / "e2e" / "desktop_test.go"
RESULT = re.compile(r"^--- (PASS|FAIL|SKIP): (Test\w*) \(([0-9.]+)s\)$")
DESKTOP_RESULT = re.compile(r"^\s*--- (PASS|FAIL|SKIP): TestDesktop/([A-Za-z0-9_-]+) \(([0-9.]+)s\)$")


def read_tests(path):
    """Top-level test names from go test -list output."""
    names = []
    for line in Path(path).read_text().splitlines():
        line = line.strip()
        if re.fullmatch(r"Test\w*", line):
            names.append(line)
    if not names:
        sys.exit(f"{path}: no tests listed")
    if len(set(names)) != len(names):
        sys.exit(f"{path}: a test is listed twice")
    return sorted(names)


def read_windows_desktop_specs(path=DESKTOP_TEST):
    """Read the authoritative windowsSpecs map from e2e/desktop_test.go."""
    text = Path(path).read_text()
    m = re.search(r"var\s+windowsSpecs\s*=\s*map\[string\]bool\{([^}]+)\}", text)
    if not m:
        sys.exit(f"{path}: windowsSpecs map not found")
    specs = re.findall(r'"([A-Za-z0-9_-]+)"\s*:\s*true', m.group(1))
    if not specs:
        sys.exit(f"{path}: windowsSpecs map is empty")
    return sorted(set(specs))


def read_times(path=TIMES):
    p = Path(path)
    if not p.exists():
        return {}
    return {k: float(v) for k, v in json.loads(p.read_text())["seconds"].items()}


def plan(tests, shards, times_path=TIMES):
    times = read_times(times_path)
    known = [times[t] for t in tests if t in times and times[t] > 0]
    default = statistics.median(known) if known else 1.0
    weight = {t: times.get(t, default) for t in tests}
    load = [0.0] * shards
    given = [[] for _ in range(shards)]
    for t in sorted(tests, key=lambda t: (-weight[t], t)):
        i = min(range(shards), key=lambda i: (load[i], i))
        given[i].append(t)
        load[i] += weight[t]
    unknown = sorted(t for t in tests if t not in times)
    return [sorted(g) for g in given], load, unknown


def pattern(names, prefix=""):
    return f"^{prefix}(" + "|".join(names) + ")$"


def results(log, rx=RESULT):
    """{test: (result, seconds)} for the tests a shard's -v log reports."""
    seen = {}
    dup = []
    if not Path(log).exists():
        print(f"{log}: missing (the shard left no log)")
        return seen, dup
    for line in Path(log).read_text(errors="replace").splitlines():
        m = rx.match(line.rstrip())
        if m:
            if m.group(2) in seen:
                dup.append(m.group(2))
            seen[m.group(2)] = (m.group(1), float(m.group(3)))
    return seen, dup


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("run-pattern", "plan", "ledger"):
        p = sub.add_parser(name)
        p.add_argument("--of", type=int, required=True, help="number of shards")
        p.add_argument("--exclude", action="append", default=[], help="top-level test name to omit from sharding (run by another job)")
        if name == "run-pattern":
            p.add_argument("--shard", type=int, required=True, help="1-based shard")
        p.add_argument("tests", help="go test -list output")
        if name == "ledger":
            p.add_argument("logs", nargs="+", help="each shard's go test -v log, shard 1 first")
    for name in ("desktop-run-pattern", "desktop-plan", "desktop-ledger"):
        p = sub.add_parser(name)
        p.add_argument("--of", type=int, required=True, help="number of shards")
        p.add_argument("--desktop-test", default=str(DESKTOP_TEST), help="path to e2e/desktop_test.go")
        if name == "desktop-run-pattern":
            p.add_argument("--shard", type=int, required=True, help="1-based shard")
        if name == "desktop-ledger":
            p.add_argument("logs", nargs="+", help="each shard's go test -v log, shard 1 first")
    a = ap.parse_args()
    if a.of < 1:
        sys.exit("--of must be at least 1")

    is_desktop = a.cmd.startswith("desktop-")
    if is_desktop:
        tests = read_windows_desktop_specs(a.desktop_test)
        times_path = DESKTOP_TIMES
        rx = DESKTOP_RESULT
        prefix = "TestDesktop$/^"
    else:
        excluded = set(a.exclude)
        tests = [t for t in read_tests(a.tests) if t not in excluded]
        if not tests:
            sys.exit(f"{a.tests}: no tests left after --exclude")
        times_path = TIMES
        rx = RESULT
        prefix = ""

    given, load, unknown = plan(tests, a.of, times_path=times_path)

    if a.cmd in ("run-pattern", "desktop-run-pattern"):
        if not 1 <= a.shard <= a.of:
            sys.exit(f"--shard must be 1..{a.of}")
        print(pattern(given[a.shard - 1], prefix=prefix))
        return

    if a.cmd in ("plan", "desktop-plan"):
        for i, g in enumerate(given):
            print(f"shard {i + 1}: {len(g)} tests, ~{load[i] / 60:.1f} min")
            for t in g:
                print(f"  {t}")
        if unknown:
            print(f"no measured time yet (median used): {', '.join(unknown)}")
        return

    # ledger / desktop-ledger
    if len(a.logs) != a.of:
        sys.exit(f"{a.of} shards but {len(a.logs)} logs")
    ok = True
    ran = {}
    lines = ["| shard | tests | ran | measured min |", "|---|---|---|---|"]
    for i, log in enumerate(a.logs):
        seen, dup = results(log, rx=rx)
        want = set(given[i])
        got = set(seen)
        for t in sorted(want - got):
            print(f"shard {i + 1}: {t} was given to it but did not report a result")
            ok = False
        for t in sorted(got - want):
            print(f"shard {i + 1}: {t} ran but was not given to it")
            ok = False
        for t in dup:
            print(f"shard {i + 1}: {t} reported twice")
            ok = False
        for t, r in seen.items():
            ran.setdefault(t, []).append((i + 1, r))
        lines.append(f"| {i + 1} | {len(want)} | {len(got)} | {sum(s for _, s in seen.values()) / 60:.1f} |")
    for t in tests:
        n = len(ran.get(t, []))
        if n != 1:
            print(f"{t}: ran {n} times across the shards (want exactly 1)")
            ok = False
    for t in sorted(set(ran) - set(tests)):
        print(f"{t}: ran but test list does not list it")
        ok = False

    counts = {}
    for t in tests:
        for _, (r, _) in ran.get(t, []):
            counts[r] = counts.get(r, 0) + 1
    print("\n".join(lines))
    print("\nlisted test, shard, result, seconds:")
    for t in tests:
        for i, (r, s) in ran.get(t, []):
            print(f"{t} {i} {r} {s:.2f}")
        if t not in ran:
            print(f"{t} - MISSING -")
    print(f"\nlisted {len(tests)}, ran once each: {sum(1 for t in tests if len(ran.get(t, [])) == 1)}; "
          + ", ".join(f"{k} {v}" for k, v in sorted(counts.items())))
    fresh = {"seconds": {t: round(ran[t][0][1][1], 2) for t in sorted(ran)}}
    label = "scripts/windows_desktop_times.json" if is_desktop else "scripts/e2e_shard_times.json"
    print(f"\nmeasured times ({label}):")
    print(json.dumps(fresh, indent=1))
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
