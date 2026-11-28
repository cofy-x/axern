"""Build the shared development toolchain and official Agent workload images."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time

from install import CONTEXT_FILES, ROOT, TARGETS, load_lock, prepare_cache, prepare_context


def arguments(description=__doc__, cache=True):
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument('--targets', default='toolchain,claude-code,codex')
    parser.add_argument('--tag-prefix', default='axern-agent')
    parser.add_argument('--tag-suffix', default='local')
    parser.add_argument('--docker-config', type=Path)
    parser.add_argument('--output-dir', type=Path)
    if cache:
        parser.add_argument('--cache-dir', type=Path)
    return parser


def selected_targets(value):
    targets = value.split(',')
    if len(set(targets)) != len(targets) or any(target not in TARGETS for target in targets):
        raise ValueError('targets must be distinct comma-separated image stage names')
    return targets


def image_tags(lock, prefix, suffix):
    if not re.fullmatch(r'[a-z0-9][a-z0-9._:/-]*', prefix) or not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]*', suffix):
        raise ValueError('invalid image tag prefix or suffix')
    records = lock['artifacts']
    return {
        'base': prefix + '-base:' + suffix,
        'toolchain': prefix + '-toolchain:' + suffix,
        'claude-code': prefix + '-claude:' + records['claude-code']['version'] + '-' + suffix,
        'codex': prefix + '-codex:' + records['codex']['version'] + '-' + suffix,
    }


def docker_command(config):
    return ['docker', '--config', str(config.resolve())] if config else ['docker']


def source_hashes(context):
    return {name: hashlib.sha256((context / name).read_bytes()).hexdigest() for name in CONTEXT_FILES}


def build_images(args):
    lock = load_lock()
    targets = selected_targets(args.targets)
    tags = image_tags(lock, args.tag_prefix, args.tag_suffix)
    output = args.output_dir.resolve() if args.output_dir else None
    if output:
        output.mkdir(parents=True, exist_ok=True)
    receipts = []
    refresh_epoch = str(time.time_ns())
    env = {key: value for key, value in os.environ.items() if not any(part in key.upper() for part in ('DEEPSEEK', 'ANTHROPIC', 'OPENAI', 'CLAUDE', 'CODEX_API_KEY'))}
    with tempfile.TemporaryDirectory(prefix='axern-agent-images-') as temporary:
        context = Path(temporary) / 'context'
        prepare_context(ROOT, context)
        cached = Path(temporary) / 'cache'
        prepare_cache(args.cache_dir.resolve() if args.cache_dir else cached, cached, lock)
        source_sha256 = source_hashes(context)
        for target in targets:
            iidfile = Path(temporary) / (target + '.iid')
            command = docker_command(args.docker_config) + [
                'build', '--platform', lock['platform'], '--build-arg', 'BASE_IMAGE=' + lock['base']['image'],
                '--build-arg', 'APT_REFRESH_EPOCH=' + refresh_epoch, '--iidfile', str(iidfile),
                '--build-context', 'runtime-cache=' + str(cached), '--target', target, '--tag', tags[target],
                '--file', str(context / 'Dockerfile'), str(context),
            ]
            started = time.monotonic()
            if output:
                with (output / (target + '-build.log')).open('wb') as log:
                    result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, env=env)
            else:
                result = subprocess.run(command, env=env)
            image_id = iidfile.read_text().strip() if result.returncode == 0 and iidfile.is_file() else None
            receipt = {
                'target': target,
                'tag': tags[target],
                'platform': lock['platform'],
                'image_id': image_id,
                'apt_refresh_epoch': refresh_epoch,
                'command': command,
                'exit_code': result.returncode,
                'elapsed_seconds': round(time.monotonic() - started, 3),
                'source_sha256': source_sha256,
            }
            receipts.append(receipt)
            if output:
                (output / 'build-receipts.json').write_text(json.dumps(receipts, indent=2) + '\n')
            if result.returncode:
                raise SystemExit(result.returncode)
            if not isinstance(image_id, str) or not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id):
                raise ValueError('Docker did not record an immutable image ID for ' + target)
    print(json.dumps({'images': {target: tags[target] for target in targets}}, indent=2))


def main():
    args = arguments().parse_args()
    try:
        build_images(args)
    except ValueError as error:
        raise SystemExit(str(error)) from error


if __name__ == '__main__':
    main()
