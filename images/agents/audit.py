"""Inventory exact local Agent image builds without running them."""

import argparse
from hashlib import sha256
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

from build import docker_command, selected_targets, source_hashes
from install import ROOT, load_lock

sys.path.insert(0, str(ROOT.parents[1] / 'scripts' / 'release'))
from delivery_image_audit import (  # noqa: E402
    INDEX_TYPES, MANIFEST_TYPES, MAX_METADATA, TOOL_VERSIONS, descriptor_identity, file_digest,
    load_json, prepare_layout, require, scan, scanner_inputs, valid_digest,
    verify_grype, verify_sbom, write_json,
)


def read_receipts(path, targets, platform):
    records = json.loads(path.read_bytes())
    require(isinstance(records, list), 'Expected an Agent image build receipt list')
    hashes = source_hashes(ROOT)
    receipts = {}
    for record in records:
        target = record['target']
        require(target in targets and target not in receipts, 'Unexpected or duplicate Agent image target')
        require(record['source_sha256'] == hashes and record['platform'] == platform,
                'Agent image receipt does not match the source or platform')
        require(record['exit_code'] == 0 and valid_digest(record['image_id']),
                'Agent build did not produce an immutable image ID')
        require(re.fullmatch(r'[0-9]{1,24}', record['apt_refresh_epoch']) is not None,
                'Agent build did not record signed-package refresh')
        receipts[target] = record
    require(receipts.keys() == set(targets), 'Agent image receipts are missing targets')
    require(len({record['apt_refresh_epoch'] for record in records}) == 1,
            'Agent image receipts came from different package refreshes')
    return receipts


class LocalArchive:
    """Supply only verified OCI blobs from a Docker image save archive."""

    def __init__(self, archive, cache):
        self.archive = archive
        self.cache = cache
        members = archive.getmembers()
        self.members = {member.name: member for member in members}
        require(len(members) == len(self.members), 'Duplicate members in Docker image archive')

    def member(self, name, limit, size=None):
        member = self.members.get(name)
        require(member is not None and member.isfile() and 0 < member.size <= limit
                and (size is None or member.size == size), 'Missing or invalid Docker image archive member ' + name)
        return self.archive.extractfile(member)

    def manifest(self, expected, os_name, architecture):
        with self.member('index.json', MAX_METADATA) as source:
            index = load_json(source.read())
        require(index.get('schemaVersion') == 2 and index.get('mediaType') in INDEX_TYPES,
                'Docker did not export an OCI image index')
        descriptors = index['manifests']
        require(isinstance(descriptors, list) and len(descriptors) == 1, 'Expected one exported workload platform')
        descriptor = descriptors[0]
        platform = descriptor['platform']
        require(descriptor['mediaType'] in MANIFEST_TYPES and platform['os'] == os_name
                and platform['architecture'] == architecture, 'Exported workload platform does not match the build')
        digest, size = descriptor_identity(descriptor, MAX_METADATA)
        require(digest == expected['digest'] and size == expected['size']
                and descriptor['mediaType'] == expected['mediaType'],
                'Exported workload manifest does not match the immutable Docker image')
        return descriptor

    def fetch(self, _kind, digest, limit, size):
        require(valid_digest(digest), 'Invalid workload OCI digest')
        path = self.cache / digest.removeprefix('sha256:')
        if not path.exists():
            with self.member('blobs/sha256/' + path.name, limit, size) as source, path.open('xb') as output:
                shutil.copyfileobj(source, output, length=1024**2)
            require(file_digest(path) == digest, 'Workload OCI blob digest mismatch')
        require(path.stat().st_size == size, 'Workload OCI blob size mismatch')
        return path


def inventory_image(docker, record, output, tools, env, settings):
    image_id = record['image_id']
    platform = record['platform']
    os_name, architecture = platform.split('/')
    inspected = subprocess.check_output(docker + ['image', 'inspect', image_id], timeout=60)
    image = json.loads(inspected)[0]
    require(image['Id'] == image_id and image['Os'] == os_name and image['Architecture'] == architecture,
            'Docker image identity or platform changed since the build')
    inspected = subprocess.check_output(docker + ['image', 'inspect', '--platform', platform, image_id], timeout=60)
    image = json.loads(inspected)[0]
    expected = image['Descriptor']
    require(image['Id'] == expected['digest'] and image['Os'] == os_name and image['Architecture'] == architecture,
            'Docker did not identify the immutable platform manifest')
    (output / 'image-inspect.json').write_bytes(inspected)

    with tempfile.TemporaryDirectory(prefix='axern-workload-oci-') as temporary:
        work = Path(temporary)
        saved = work / 'image.tar'
        command = docker + ['image', 'save', '--platform', platform, '-o', str(saved), image_id]
        with (output / 'docker-save.stderr').open('xb') as errors:
            subprocess.run(command, check=True, stderr=errors, timeout=900)
        cache = work / 'cache'
        cache.mkdir()
        layout = work / 'oci'
        with tarfile.open(saved, 'r:') as archive:
            source = LocalArchive(archive, cache)
            descriptor = source.manifest(expected, os_name, architecture)
            manifest, config = prepare_layout(source.fetch, descriptor, architecture, layout, output)
        require(image['RootFS']['Layers'] == config['rootfs']['diff_ids'], 'Workload image layer identities differ')
        sbom = scan([tools['syft'], 'scan', f'oci-dir:{layout}', '--config', str(settings), '-o', 'syft-json'],
                    env, output, 'syft')
        verify_sbom(sbom, expected['digest'], manifest, config)

    return sbom, {
        'manifest': expected['digest'], 'config': manifest['config']['digest'],
        'diff_ids': config['rootfs']['diff_ids'], 'packages': len(sbom['artifacts']),
        'sbom_digest': file_digest(output / 'syft.json'),
    }


def prior_images(directory, digest, receipts, summary):
    raw = (directory / 'summary.json').read_bytes()
    require('sha256:' + sha256(raw).hexdigest() == digest, 'Prior audit summary digest differs')
    prior = load_json(raw)
    require(prior.get('complete') is True, 'Prior audit is incomplete')
    require(prior.get('build_receipts_digest') == summary['build_receipts_digest'],
            'Prior audit build receipts digest differs')
    require(all(prior.get(key) == summary[key] for key in ('source_sha256', 'apt_refresh_epoch')),
            'Prior audit build recipe or refresh identity differs')
    require(prior['tools']['syft']['version'] == TOOL_VERSIONS['syft'], 'Prior audit Syft version differs')
    images = prior['images']
    require(isinstance(images, list), 'Expected a prior audit image list')
    by_target = {}
    for image in images:
        target = image['target']
        require(target in receipts and target not in by_target, 'Unexpected or duplicate prior audit target')
        require(image.get('complete') is True and
                all(image.get(key) == receipts[target][key] for key in ('image_id', 'platform')),
                'Prior audit target is incomplete or has a different image identity or platform')
        require(valid_digest(image.get('sbom_digest')), 'Prior audit lacks a valid SBOM digest for ' + target)
        by_target[target] = image
    require(by_target.keys() == receipts.keys(), 'Prior audit is missing targets')
    return by_target


def copy_inventory_file(source, output, name, digest):
    raw = (source / name).read_bytes()
    require('sha256:' + sha256(raw).hexdigest() == digest, 'Prior inventory digest differs for ' + name)
    (output / name).write_bytes(raw)
    return load_json(raw)


def reuse_image(source, output, image):
    manifest = copy_inventory_file(source, output, 'manifest.json', image['manifest'])
    config = copy_inventory_file(source, output, 'config.json', image['config'])
    sbom = copy_inventory_file(source, output, 'syft.json', image['sbom_digest'])
    require(manifest['config']['digest'] == image['config'] and
            config['os'] + '/' + config['architecture'] == image['platform'] and
            config['rootfs']['diff_ids'] == image['diff_ids'],
            'Prior inventory config, platform or layers differ from the audit')
    verify_sbom(sbom, image['manifest'], manifest, config)
    require(len(sbom['artifacts']) == image['packages'], 'Prior audit package count differs from the SBOM')
    return sbom, {key: image[key] for key in ('manifest', 'config', 'diff_ids', 'packages', 'sbom_digest')}


def scan_findings(sbom, output, tools, env, settings, status):
    findings = scan([tools['grype'], f'sbom:{output / "syft.json"}', '--config', str(settings), '-o', 'json'],
                    env, output, 'grype')
    verify_grype(findings, sbom, status)
    matches, ignored = findings['matches'], findings.get('ignoredMatches', [])
    require(isinstance(matches, list) and isinstance(ignored, list), 'Incomplete workload findings')
    severities = ('Critical', 'High', 'Medium', 'Low', 'Negligible', 'Unknown')
    counts = dict.fromkeys(severities, 0)
    for match in matches + ignored:
        counts[match['vulnerability']['severity']] += 1
    return {'matches': len(matches), 'ignored_matches': len(ignored), 'severity_counts': counts}


def audit(args):
    targets = selected_targets(args.targets)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    summary_path = output / 'summary.json'
    summary = {'complete': False, 'images': []}
    try:
        raw_receipts = args.receipts.read_bytes()
        receipts_path = output / 'build-receipts.json'
        receipts_path.write_bytes(raw_receipts)
        receipts = read_receipts(receipts_path, targets, load_lock()['platform'])
        summary['build_receipts_digest'] = file_digest(receipts_path)
        summary['source_sha256'] = receipts[targets[0]]['source_sha256']
        summary['apt_refresh_epoch'] = receipts[targets[0]]['apt_refresh_epoch']
        reused = None
        if args.reuse_inventory is not None:
            reused = prior_images(args.reuse_inventory, args.inventory_digest, receipts, summary)
            summary['inventory_basis'] = {'summary_digest': args.inventory_digest}
        db, tools, env, settings, status = scanner_inputs(output, args.tools, args.database, summary)
        if reused is None:
            docker = docker_command(args.docker_config)
        for target in targets:
            report = {'target': target, 'image_id': receipts[target]['image_id'], 'complete': False}
            summary['images'].append(report)
            path = output / target
            path.mkdir()
            try:
                if reused is None:
                    sbom, inventory = inventory_image(docker, receipts[target], path, tools, env, settings)
                else:
                    sbom, inventory = reuse_image(args.reuse_inventory / target, path, reused[target])
                report.update(inventory, platform=receipts[target]['platform'])
                report.update(scan_findings(sbom, path, tools, env, settings, status), complete=True)
            except (KeyError, OSError, TypeError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
                report['error'] = str(error)
        require(file_digest(db) == summary['database']['digest'], 'Grype database changed during the audit')
        summary['complete'] = all(report['complete'] for report in summary['images'])
    except (KeyError, OSError, TypeError, ValueError, subprocess.SubprocessError) as error:
        summary['error'] = str(error)
    finally:
        write_json(summary_path, summary)
    print(json.dumps({'complete': summary['complete'], 'report': str(summary_path),
                      'summary_digest': file_digest(summary_path)}))
    return 0 if summary['complete'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--targets', default='base,toolchain,claude-code,codex')
    parser.add_argument('--receipts', type=Path, required=True)
    parser.add_argument('--tools', type=Path, required=True)
    parser.add_argument('--database', type=Path, required=True)
    parser.add_argument('--docker-config', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--reuse-inventory', type=Path, help='Completed audit directory')
    parser.add_argument('--inventory-digest', help='Independently saved prior summary_digest')
    args = parser.parse_args()
    if (args.reuse_inventory is None) != (args.inventory_digest is None):
        parser.error('--reuse-inventory and --inventory-digest must be supplied together')
    if args.inventory_digest is not None and not valid_digest(args.inventory_digest):
        parser.error('--inventory-digest must be a sha256 digest')
    return audit(args)


if __name__ == '__main__':
    raise SystemExit(main())
