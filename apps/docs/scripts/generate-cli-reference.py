#!/usr/bin/env python3
"""Render the current CLI's command groups without a recording toolchain."""
from html import escape
from pathlib import Path
import re
import subprocess

repo = Path(__file__).resolve().parents[3]
help_text = subprocess.check_output([str(repo / 'bin/axern'), '--help'], text=True, timeout=10)
rows = [line.strip() for line in help_text.splitlines()
        if re.match(r'^  (dev|server|workspace|run|ssh|tunnel)\s', line)]
if len(rows) != 6:
    raise SystemExit('CLI help must expose exactly the six documented command groups')
version = (repo / 'VERSION').read_text().strip()
lines = ['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1100 350" role="img" aria-labelledby="title">',
         '<title id="title">Axern CLI command groups</title>',
         '<rect width="1100" height="350" rx="8" fill="#101927"/>',
         '<g fill="#e6edf3" font-family="ui-monospace, monospace" font-size="16">',
         f'<text x="28" y="38">Axern {escape(version)} · source CLI help</text>',
         '<text x="28" y="76" fill="#7dd3fc">$ axern --help</text>']
for index, row in enumerate(rows):
    command, description = re.split(r'\s{2,}', row, maxsplit=1)
    y = 114 + index * 35
    lines.append(f'<text x="28" y="{y}" fill="#7dd3fc">{escape(command)}</text>')
    lines.append(f'<text x="176" y="{y}">{escape(description)}</text>')
lines.extend(['</g>', '</svg>'])
output = repo / 'apps/docs/public/terminal/axern.svg'
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text('\n'.join(lines) + '\n')
