#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path

from allow_builds import add_allow_builds, ensure_all_builds, parse_allow_keys, strip_trailing_version

PNPM_LOG = """
[ERR_PNPM_GIT_DEP_PREPARE_NOT_ALLOWED] Failed to prepare git-hosted package fetched from "https://codeload.github.com/cocofhu/anime-find/tar.gz/87ea349769e8631a187b145387bc43e13776dece": The git-hosted package "@cocofhu/anime-find@0.1.11" needs to execute build scripts but is not in the "allowBuilds" allowlist.

Add the package to "allowBuilds" in your project's pnpm-workspace.yaml to allow it to run scripts. For example:
allowBuilds:
  @cocofhu/anime-find@https://codeload.github.com/cocofhu/anime-find/tar.gz/87ea349769e8631a187b145387bc43e13776dece: true
"""


class AllowBuildsTest(unittest.TestCase):
    def test_strip_version(self):
        self.assertEqual(strip_trailing_version("@cocofhu/anime-find@0.1.11"), "@cocofhu/anime-find")
        self.assertEqual(strip_trailing_version("foo@1.0.0"), "foo")

    def test_parse_pnpm_example(self):
        keys = parse_allow_keys(PNPM_LOG)
        self.assertIn(
            "@cocofhu/anime-find@https://codeload.github.com/cocofhu/anime-find/tar.gz/87ea349769e8631a187b145387bc43e13776dece",
            keys,
        )
        self.assertIn(
            "@cocofhu/anime-find@git+https://github.com/cocofhu/anime-find.git",
            keys,
        )

    def test_parse_unrelated_log(self):
        self.assertEqual(parse_allow_keys("pnpm not found"), [])

    def test_ensure_and_merge(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "pnpm-workspace.yaml"
            path.write_text("packages:\n  - .\n\nnodeLinker: hoisted\n")
            self.assertTrue(ensure_all_builds(path))
            self.assertFalse(ensure_all_builds(path))
            text = path.read_text()
            self.assertIn("dangerouslyAllowAllBuilds: true", text)

            added = add_allow_builds(
                path,
                [
                    "@cocofhu/anime-find@https://codeload.github.com/cocofhu/anime-find/tar.gz/abc",
                    "@cocofhu/anime-find@git+https://github.com/cocofhu/anime-find.git",
                ],
            )
            self.assertEqual(len(added), 2)
            self.assertEqual(add_allow_builds(path, added), [])
            merged = path.read_text()
            self.assertIn("allowBuilds:", merged)
            self.assertIn("@cocofhu/anime-find@git+https://github.com/cocofhu/anime-find.git", merged)
            self.assertIn("dangerouslyAllowAllBuilds: true", merged)


if __name__ == "__main__":
    unittest.main()
