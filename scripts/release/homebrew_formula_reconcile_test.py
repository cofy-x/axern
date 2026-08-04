#!/usr/bin/env python3

from __future__ import annotations

import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import homebrew_formula_reconcile as formula


def content(version: str, suffix: str = "") -> bytes:
    return f'class Axern < Formula\n  version "{version}"\n{suffix}end\n'.encode()


class HomebrewFormulaReconcileTest(unittest.TestCase):
    def test_creates_missing_formula_and_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            candidate = root / "candidate.rb"
            target = root / "Formula" / "axern.rb"
            candidate.write_bytes(content("1.2.3"))
            self.assertTrue(formula.reconcile(candidate, target))
            self.assertEqual(target.read_bytes(), candidate.read_bytes())
            self.assertFalse(formula.reconcile(candidate, target))

    def test_upgrades_older_formula(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            candidate = root / "candidate.rb"
            target = root / "axern.rb"
            candidate.write_bytes(content("1.3.0"))
            target.write_bytes(content("1.2.9"))
            self.assertTrue(formula.reconcile(candidate, target))
            self.assertEqual(target.read_bytes(), candidate.read_bytes())

    def test_rejects_downgrade(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            candidate = root / "candidate.rb"
            target = root / "axern.rb"
            candidate.write_bytes(content("1.2.3"))
            target.write_bytes(content("1.3.0"))
            with self.assertRaisesRegex(formula.FormulaError, "downgrade"):
                formula.reconcile(candidate, target)

    def test_rejects_same_version_with_different_content(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            candidate = root / "candidate.rb"
            target = root / "axern.rb"
            candidate.write_bytes(content("1.2.3", "  desc \"candidate\"\n"))
            target.write_bytes(content("1.2.3", "  desc \"different\"\n"))
            with self.assertRaisesRegex(formula.FormulaError, "different content"):
                formula.reconcile(candidate, target)


if __name__ == "__main__":
    unittest.main()
