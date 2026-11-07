"""Check locked image inputs and run host-safe boundary tests without Docker."""

from pathlib import Path
import re
import unittest

from install import CONTEXT_FILES, ROOT, archive_name, load_lock


def main():
    lock = load_lock()
    workspace = ROOT.parents[1]
    go_version = re.search(r'^go (\S+)$', (workspace / 'go.work').read_text(), re.MULTILINE)
    if not go_version or go_version.group(1) != lock['artifacts']['go']['version']:
        raise SystemExit('Agent toolchain Go version must match go.work')
    names = [archive_name(record) for record in lock['artifacts'].values()]
    if len(names) != len(set(names)):
        raise SystemExit('runtime cache filenames must be unambiguous')
    allowed = {line[1:] for line in (ROOT / '.dockerignore').read_text().splitlines() if line.startswith('!')}
    if allowed != set(CONTEXT_FILES):
        raise SystemExit('Docker context allowlist must match staged source files')
    suite = unittest.defaultTestLoader.discover(str(ROOT), pattern='test_*.py')
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    if not result.wasSuccessful():
        raise SystemExit(1)
    print('Agent image lock and host-safe boundary checks passed')


if __name__ == '__main__':
    main()
