#!/usr/bin/env python3
"""Unit tests for scripts/e2e_shards.py (work order #125)."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SHARDS = ROOT / "scripts" / "e2e_shards.py"


class E2EShardsTest(unittest.TestCase):
    def _run(self, *args):
        return subprocess.run(
            [sys.executable, str(SHARDS), *args],
            capture_output=True,
            text=True,
        )

    def test_sharding_partition_and_exclude_parity(self):
        tests = [f"TestCase{i:02d}" for i in range(20)] + ["TestDesktop", "TestWindowsInstall"]
        with tempfile.TemporaryDirectory() as td:
            tf = Path(td) / "tests.txt"
            tf.write_text("\n".join(tests) + "\n")

            # 6 shards covering all tests
            seen = []
            for s in range(1, 7):
                r = self._run("run-pattern", "--of", "6", "--shard", str(s), str(tf))
                self.assertEqual(r.returncode, 0, r.stderr)
                pat = r.stdout.strip()
                self.assertTrue(pat.startswith("^(") and pat.endswith(")$"))
                seen.extend(pat[2:-2].split("|"))
            self.assertEqual(sorted(seen), sorted(tests))

            # 4 shards excluding TestDesktop (windows-e2e)
            win_seen = []
            logs = []
            for s in range(1, 5):
                r = self._run("run-pattern", "--exclude", "TestDesktop", "--of", "4", "--shard", str(s), str(tf))
                self.assertEqual(r.returncode, 0, r.stderr)
                names = r.stdout.strip()[2:-2].split("|")
                self.assertNotIn("TestDesktop", names)
                win_seen.extend(names)
                lf = Path(td) / f"shard-{s}.log"
                lf.write_text("\n".join(f"--- PASS: {n} (1.50s)" for n in names) + "\n")
                logs.append(str(lf))
            want_win = [t for t in tests if t != "TestDesktop"]
            self.assertEqual(sorted(win_seen), sorted(want_win))

            # Ledger must pass when every non-desktop test ran once
            lr = self._run("ledger", "--exclude", "TestDesktop", "--of", "4", str(tf), *logs)
            self.assertEqual(lr.returncode, 0, lr.stdout + lr.stderr)
            self.assertIn(f"listed {len(want_win)}, ran once each: {len(want_win)}", lr.stdout)

            # Ledger must fail if a test is missing
            Path(logs[0]).write_text("")
            bad = self._run("ledger", "--exclude", "TestDesktop", "--of", "4", str(tf), *logs)
            self.assertNotEqual(bad.returncode, 0)

    def test_windows_desktop_sharding_and_ledger(self):
        with tempfile.TemporaryDirectory() as td:
            seen = []
            logs = []
            for s in (1, 2):
                r = self._run("desktop-run-pattern", "--of", "2", "--shard", str(s))
                self.assertEqual(r.returncode, 0, r.stderr)
                pat = r.stdout.strip()
                self.assertTrue(pat.startswith("^TestDesktop$/^(") and pat.endswith(")$"), pat)
                specs = pat[len("^TestDesktop$/^("):-2].split("|")
                self.assertGreater(len(specs), 0)
                seen.extend(specs)
                lf = Path(td) / f"desk-{s}.log"
                lf.write_text("\n".join(f"    --- PASS: TestDesktop/{sp} (12.34s)" for sp in specs) + "\n")
                logs.append(str(lf))
            self.assertEqual(len(seen), len(set(seen)))
            self.assertGreaterEqual(len(seen), 16)

            lr = self._run("desktop-ledger", "--of", "2", *logs)
            self.assertEqual(lr.returncode, 0, lr.stdout + lr.stderr)
            self.assertIn(f"listed {len(seen)}, ran once each: {len(seen)}", lr.stdout)

            # Missing spec must fail ledger
            Path(logs[1]).write_text("")
            bad = self._run("desktop-ledger", "--of", "2", *logs)
            self.assertNotEqual(bad.returncode, 0)


if __name__ == "__main__":
    unittest.main()
