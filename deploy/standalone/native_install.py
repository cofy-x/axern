#!/usr/bin/env python3
"""Install one native Axern candidate on a dedicated Ubuntu 24.04 Linux host."""

from __future__ import annotations

import argparse
import base64
import fcntl
import grp
import ipaddress
import json
import os
import pathlib
import platform
import pwd
import re
import secrets
import shutil
import socket
import stat
import subprocess
import tarfile
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
ETC = pathlib.Path('/etc/axern')
DATA = pathlib.Path('/var/lib/axern')
OPT = pathlib.Path('/opt/axern')
UNITS = pathlib.Path('/etc/systemd/system')
PG = pathlib.Path('/usr/lib/postgresql/16/bin')
TARGET = 'axern-native.target'
SERVICES = ('postgres', 'bootstrap', 'controld', 'retention', 'tunneld', 'imagefsd', 'imagemgr', 'egressd', 'axnoded', 'node-tunneld', 'gatewayd')
USERS = ('postgres', 'control', 'retention', 'tunnel', 'imagefsd', 'gateway')
PORTS = (25432, 24000, 24001, 24002, 24100, 24010, 23001, 25000, 25080, 9876)
SETTINGS = {'node_id', 'memory_budget_bytes', 'memory_reserve_bytes', 'storage_reserve_bytes', 'sandbox_cidr', 'dns_nameservers', 'max_instances'}


class InstallError(ValueError):
    pass


def run(*args: str, **kwargs) -> subprocess.CompletedProcess:
    return subprocess.run(args, check=True, **kwargs)


def read_json(path: pathlib.Path) -> dict:
    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise InstallError(f'duplicate JSON field: {key}')
            result[key] = value
        return result

    with path.open() as stream:
        value = json.load(stream, object_pairs_hook=unique_object)
    if not isinstance(value, dict):
        raise InstallError(f'expected JSON object: {path}')
    return value


def validate_settings(value: dict) -> dict:
    if set(value) != SETTINGS:
        raise InstallError('configuration must contain exactly the documented settings')
    if not isinstance(value['node_id'], str) or not re.fullmatch(r'[a-z][a-z0-9-]{2,62}', value['node_id']):
        raise InstallError('invalid explicit node_id')
    for name in ('memory_budget_bytes', 'memory_reserve_bytes', 'storage_reserve_bytes', 'max_instances'):
        if type(value[name]) is not int or value[name] <= 0:
            raise InstallError(f'{name} must be a positive integer')
    if value['memory_reserve_bytes'] <= 512 * 1024**2 or value['memory_budget_bytes'] <= value['memory_reserve_bytes'] + 512 * 1024**2:
        raise InstallError('memory budget must cover the explicit reserve and workload capacity; reserve must exceed the 512 MiB conformance ceiling')
    if not 1 <= value['max_instances'] <= 64:
        raise InstallError('max_instances must be in [1, 64]')
    if not isinstance(value['sandbox_cidr'], str):
        raise InstallError('sandbox_cidr must be a string')
    interface = ipaddress.ip_interface(value['sandbox_cidr'])
    if interface.version != 4 or not interface.ip.is_private or interface.ip.is_loopback or interface.ip in (interface.network.network_address, interface.network.broadcast_address):
        raise InstallError('sandbox_cidr must be a private IPv4 bridge address with a usable host part')
    if interface.network.num_addresses - 3 < value['max_instances']:
        raise InstallError('sandbox_cidr is too small for max_instances')
    if not isinstance(value['dns_nameservers'], list) or not 1 <= len(value['dns_nameservers']) <= 3:
        raise InstallError('supply one to three explicit non-loopback DNS servers')
    for address in value['dns_nameservers']:
        if not isinstance(address, str):
            raise InstallError('DNS servers must be IP address strings')
        ip = ipaddress.ip_address(address)
        if ip.is_loopback or ip.is_unspecified or ip.is_multicast:
            raise InstallError('DNS servers must be reachable outside the host loopback namespace')
    return value


def safe_directory(path: pathlib.Path, mode: int = 0o755) -> None:
    for parent in reversed((path, *path.parents)):
        if parent.is_symlink():
            raise InstallError(f'refusing symlink in managed path: {parent}')
    created = not path.exists()
    path.mkdir(mode=mode, parents=True, exist_ok=True)
    if created:
        path.chmod(mode)
    info = path.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise InstallError(f'managed parent must be a root-owned, non-writable directory: {path}')


def write_file(path: pathlib.Path, data: bytes, mode: int = 0o600, group: str | None = None, replace: bool = False) -> None:
    if path.is_symlink():
        raise InstallError(f'refusing symlink: {path}')
    if path.exists():
        info = path.stat()
        gid = grp.getgrnam(group).gr_gid if group else os.getegid()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_gid != gid
                or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != mode):
            raise InstallError(f'managed file ownership or permissions changed: {path}')
        if not replace and path.read_bytes() != data:
            raise InstallError(f'existing managed file differs: {path}')
        if not replace:
            return
    descriptor, temporary = tempfile.mkstemp(prefix='.axern-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'wb') as output:
            output.write(data)
            output.flush()
            os.fchmod(output.fileno(), mode)
            if group:
                os.fchown(output.fileno(), 0, grp.getgrnam(group).gr_gid)
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def json_bytes(value: dict) -> bytes:
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()


def unit_name(service: str) -> str:
    return f'axern-native-{service}.service'


def data_identity() -> dict:
    if DATA.is_symlink() or not DATA.is_dir():
        raise InstallError('owned data directory is missing or replaced')
    uuid = subprocess.check_output(['findmnt', '-no', 'UUID', '--mountpoint', str(DATA)], text=True).strip()
    if not uuid:
        raise InstallError('data filesystem must have a stable UUID')
    return {'filesystem_uuid': uuid, 'directory_inode': DATA.stat().st_ino}


def verify_installed(check_data: bool = True) -> dict:
    identity = read_json(ETC / 'installation.json')
    if ROOT != OPT / 'releases' / identity['archive_sha256'] or (ETC / 'installed').read_text().strip() != identity['archive_sha256']:
        raise InstallError('incomplete installation or wrong installed helper')
    if check_data and data_identity() != identity['data_identity']:
        raise InstallError('data filesystem/directory identity changed; restore or remount the owned data before starting')
    return identity


def command(args: list[str]) -> str:
    # Only fixed paths and validated configuration enter unit command lines.
    return ' '.join('"' + arg.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%').replace('$', '$$') + '"' for arg in args)


def service_units(root: pathlib.Path, settings: dict) -> dict[str, str]:
    def binary(name):
        return str(root / 'bin' / name)

    def credential(role, name):
        return str(ETC / 'credentials' / role / name)

    def tls(role):
        return ['-tls-ca-cert', str(ETC / 'ca.crt'), '-workload-cluster', 'axern.local', '-workload-bundle', credential(role, 'bundle.pem')]

    helper = ['/usr/bin/python3', str(root / 'libexec/axern/native_install.py')]
    definitions = {
        'postgres': ('postgres', [], [str(PG / 'postgres'), '-D', str(DATA / 'postgres'), '-c', 'config_file=' + str(ETC / 'postgres.conf')]),
        'bootstrap': (None, ['postgres'], helper + ['internal-bootstrap']),
        'controld': ('control', ['bootstrap'], [binary('controld'), '-grpc-address', '127.0.0.1:24000', '-http-address', '127.0.0.1:24001', '-enrollment-address', '127.0.0.1:24002', '-workload-signer-bundle', credential('control', 'signer.pem'), '-tunnel-relays', 'default,127.0.0.1:25000,127.0.0.1:24100,1,false'] + tls('control')),
        'retention': ('retention', ['bootstrap'], [binary('controld-retention')]),
        'tunneld': ('tunnel', ['controld'], [binary('tunneld'), '-listen', '127.0.0.1:24100', '-control-target', '127.0.0.1:24000', '-relay-tls-cert', credential('tunnel', 'bundle.pem'), '-relay-tls-key', credential('tunnel', 'bundle.pem')] + tls('tunnel')),
        'imagefsd': ('imagefsd', [], [binary('imagefsd'), 'serve-chunk', '--chunk-db-dir', str(DATA / 'imagemgr/chunk_db'), '--listen-address', '127.0.0.1', '--listen-port', '9876', '--chunk-server-sock', str(DATA / 'imagemgr/chunk_db/chunkserver.sock'), '--log-file', str(DATA / 'imagemgr/chunk_db/server.log')]),
        'imagemgr': (None, ['imagefsd'], [binary('imagemgr'), '-root', str(DATA / 'imagemgr'), '-node_id', settings['node_id'], '-imagefsd_bin', binary('imagefsd'), '-nydus_template', str(root / 'share/axern/nydus_registry.json.example'), '-registry_auths_path', str(root / 'share/axern/registry_auths.json'), '-http_sock', '/run/axern-native-imagemgr/imagemgr.sock']),
        'egressd': (None, [], [binary('egressd'), '-root', str(DATA / 'egressd'), '-socket', '/run/axern-native-egressd/egressd.sock']),
        'axnoded': (None, ['controld', 'imagemgr', 'egressd'], [binary('axnoded'), '-root', str(DATA / 'axnoded'), '-config', str(ETC / 'node.toml'), '-enrollment-token-file', str(ETC / 'private/enrollment-token'), '-socket', '/run/axern-native-axnoded/axnoded.sock', '-tunnel-socket', '/run/axern-native-axnoded/tunnel.sock', '-grpc-address', '127.0.0.1:24010', '-http-address', '127.0.0.1:23001', '-log-file', str(DATA / 'axnoded/axnoded.log')]),
        'node-tunneld': (None, ['axnoded', 'tunneld'], [binary('node-tunneld'), '-node-id', settings['node_id'], '-control-target', '127.0.0.1:24000', '-tunnel-socket', '/run/axern-native-axnoded/tunnel.sock', '-tls-ca-cert', str(ETC / 'ca.crt'), '-identity-bundle', str(DATA / 'axnoded/root/identity/node.pem'), '-workload-cluster', 'axern.local', '-relay-tls-ca-cert', str(ETC / 'ca.crt'), '-runsc-binary', str(OPT / 'gvisor/runsc'), '-runsc-root', str(DATA / 'axnoded/root/runsc'), '-agent-binary', str(root / 'libexec/axnoded/tunnel-agent')]),
        'gatewayd': ('gateway', ['controld', 'tunneld'], [binary('gatewayd'), '-http-address', '127.0.0.1:25080', '-control-edge-address', '127.0.0.1:25000', '-control-edge-tls-ca-cert', str(ETC / 'ca.crt'), '-control-edge-tls-cert', credential('gateway', 'bundle.pem'), '-control-edge-tls-key', credential('gateway', 'bundle.pem'), '-control-target', '127.0.0.1:24000', '-tunnel-relay-target', '127.0.0.1:24100', '-tunnel-relay-tls-ca-cert', str(ETC / 'ca.crt')] + tls('gateway')),
    }
    units = {}
    for name, (user, dependencies, args) in definitions.items():
        after = ' '.join(unit_name(item) for item in dependencies)
        lines = ['[Unit]', f'Description=Axern native {name}', f'PartOf={TARGET}', 'After=network.target ' + after, 'Wants=' + after, f'RequiresMountsFor={DATA}', '[Service]', 'Type=oneshot' if name == 'bootstrap' else 'Type=simple', 'Slice=axern.slice', 'UMask=0077', f'RuntimeDirectory=axern-native-{name}', 'RuntimeDirectoryMode=0750', 'TimeoutStartSec=180', 'TimeoutStopSec=120', 'ExecStart=' + command(args)]
        lines += ['ExecStartPre=' + command(helper + ['internal-verify'])]
        if user:
            lines += [f'User=axern-{user}', f'Group=axern-{user}', 'NoNewPrivileges=yes', 'ProtectHome=yes', 'PrivateTmp=yes']
        if name == 'bootstrap':
            lines += ['RemainAfterExit=yes']
        else:
            lines += ['Restart=on-failure', 'RestartSec=3']
        if name in ('controld', 'retention'):
            lines += [f'EnvironmentFile={credential(user, "environment") }']
        if name == 'postgres':
            lines += ['KillSignal=SIGINT', 'ExecStartPost=' + command(helper + ['internal-ready', 'postgres'])]
        if name in ('imagefsd', 'imagemgr', 'egressd', 'axnoded'):
            lines += ['ExecStartPost=' + command(helper + ['internal-ready', name])]
        if name == 'axnoded':
            lines += ['Delegate=yes', 'KillMode=mixed', f'Environment=AXERN_SANDBOXD_BINARY={root}/libexec/axnoded/axern-sandboxd', f'Environment=AXERN_EROFS_FIXTURE={DATA}/fixtures/minimal.erofs']
        units[unit_name(name)] = '\n'.join(lines) + '\n'
    units[TARGET] = '[Unit]\nDescription=Axern native candidate\nWants=' + ' '.join(unit_name(name) for name in SERVICES) + '\nAfter=' + ' '.join(unit_name(name) for name in SERVICES) + '\n[Install]\nWantedBy=multi-user.target\n'
    units['axern.slice'] = f'[Unit]\nDescription=Axern native memory budget\n[Slice]\nMemoryMax={settings["memory_budget_bytes"]}\n'
    return units


def node_config(settings: dict) -> str:
    return f'''rootDir = "{DATA}/axnoded/root"
storeDir = "{DATA}/axnoded/store"
[plugin]
control_plane_target = "127.0.0.1:24000"
control_plane_enrollment_target = "127.0.0.1:24002"
workload_cluster = "axern.local"
control_plane_node_id = "{settings['node_id']}"
control_plane_node_target = "127.0.0.1:24010"
control_plane_heartbeat_interval = "1s"
control_plane_tls_ca_cert = "{ETC}/ca.crt"
[plugin.network]
ip_range = "{settings['sandbox_cidr']}"
nat_backend = "iptables"
[plugin.resource]
cgroup_cache_size = 0
interface_cache_size = 1
cgroup_root_name = "sandbox"
max_instance_num = {settings['max_instances']}
memory_system_reserve_bytes = {settings['memory_reserve_bytes']}
[plugin.runtime]
image_lib_dir = "{DATA}/axnoded/rootfs"
image_manager_socket = "/run/axern-native-imagemgr/imagemgr.sock"
egress_manager_socket = "/run/axern-native-egressd/egressd.sock"
cgroup_enforcement = "required"
filestore_mode = "existing"
filestore_dir = "{DATA}"
filestore_system_reserve_bytes = {settings['storage_reserve_bytes']}
ephemeral_storage_default_limit_bytes = 268435456
[plugin.runtime.dns]
nameservers = {json.dumps(settings['dns_nameservers'])}
[plugin.runtime.runsc]
binary = "{OPT}/gvisor/runsc"
[plugin.runtime.runsc.options]
allow_suid = true
'''


def preflight(settings: dict, first_install: bool) -> None:
    release = platform.freedesktop_os_release()
    if platform.system() != 'Linux' or release.get('ID') != 'ubuntu' or release.get('VERSION_ID') != '24.04':
        raise InstallError('this candidate requires Ubuntu 24.04 Linux')
    if not pathlib.Path('/run/systemd/system').is_dir() or not pathlib.Path('/sys/fs/cgroup/cgroup.controllers').exists():
        raise InstallError('systemd and cgroup v2 are required')
    for name in ('busybox', 'mkfs.erofs', 'ip', 'iptables', 'nft', 'mount', 'findmnt', 'useradd', 'runuser', 'systemctl', 'curl'):
        if not shutil.which(name):
            raise InstallError(f'missing host prerequisite: {name}')
    for name in ('initdb', 'postgres', 'pg_isready', 'createdb', 'psql'):
        if not (PG / name).is_file():
            raise InstallError('PostgreSQL 16 server/client tools are required')
    if pathlib.Path('/proc/sys/net/ipv4/ip_forward').read_text().strip() != '1':
        raise InstallError('enable IPv4 forwarding on this dedicated host before installation')
    mount = subprocess.run(['findmnt', '-no', 'FSTYPE', '--mountpoint', str(DATA)], capture_output=True, text=True)
    if mount.returncode != 0:
        raise InstallError(f'mount the dedicated data filesystem at {DATA} before installation')
    fs = mount.stdout.strip()
    if fs not in ('ext4', 'xfs'):
        raise InstallError('the data path must be on ext4 or XFS')
    if first_install:
        network = ipaddress.ip_interface(settings['sandbox_cidr']).network
        routes = json.loads(subprocess.check_output(['ip', '-j', '-4', 'route', 'show', 'table', 'all']))
        for route in routes:
            if route.get('dst') not in (None, 'default') and network.overlaps(ipaddress.ip_network(route['dst'], strict=False)):
                raise InstallError('sandbox range overlaps an existing host route')
        if pathlib.Path('/sys/class/net/sandbox0').exists():
            raise InstallError('sandbox0 already exists; another runtime may own host networking')
        for port in PORTS:
            with socket.socket() as listener:
                listener.bind(('127.0.0.1', port))


def owned_data_directory(path: pathlib.Path, user: str) -> None:
    if path.is_symlink():
        raise InstallError(f'refusing data symlink: {path}')
    path.mkdir(parents=True, exist_ok=True, mode=0o750)
    account = pwd.getpwnam(user)
    os.chown(path, account.pw_uid, account.pw_gid)
    path.chmod(0o750)


def credentials(role: str, name: str, data: bytes, replace: bool = False) -> None:
    directory = ETC / 'credentials' / role
    safe_directory(directory, 0o750)
    os.chown(directory, 0, grp.getgrnam('axern-' + role).gr_gid)
    write_file(directory / name, data, 0o640, 'axern-' + role, replace)


def sync_identity(renew: bool = False) -> None:
    pki = ETC / 'private/pki'
    args = [str(ROOT / 'bin/controld-workload-pki'), '-directory', str(pki), '-cluster', 'axern.local', '-dns', 'localhost', '-ips', '127.0.0.1']
    if renew:
        args += ['-renew-services']
    run(*args)
    write_file(ETC / 'ca.crt', (pki / 'ca.crt').read_bytes(), 0o644)
    for role, source in (('control', 'controld'), ('gateway', 'gatewayd'), ('tunnel', 'tunneld')):
        credentials(role, 'bundle.pem', (pki / (source + '.pem')).read_bytes(), replace=renew)
    credentials('control', 'signer.pem', (pki / 'private/signer.pem').read_bytes())


def prepare_runtime() -> None:
    if not (OPT / 'gvisor').exists():
        allowed = {'runsc', 'containerd-shim-runsc-v1', 'gvisor-bin/checkpointgofer', 'gvisor-bin/gvisor-sentry-prewarmer', 'gvisor-bin/gvisor_sentry', 'gvisor-bin/runsc-fd-parking', 'gvisor-bin/runsc-metric-server'}
        with tempfile.TemporaryDirectory(prefix='.gvisor-', dir=OPT) as temporary:
            stage = pathlib.Path(temporary) / 'runtime'
            stage.mkdir()
            seen = set()
            with tarfile.open(ROOT / 'runtime/gvisor.tar.bz2', 'r|bz2') as archive:
                for member in archive:
                    if member.isdir() and member.name.rstrip('/') == 'gvisor-bin':
                        continue
                    if member.name not in allowed or member.name in seen or not member.isfile() or member.size > 1024**3:
                        raise InstallError('unexpected gVisor runtime layout')
                    target = stage / member.name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    with archive.extractfile(member) as source, target.open('xb') as output:
                        shutil.copyfileobj(source, output)
                    target.chmod(0o755)
                    seen.add(member.name)
            if seen != allowed:
                raise InstallError('incomplete gVisor runtime')
            stage.rename(OPT / 'gvisor')
    fixtures = DATA / 'fixtures'
    if not fixtures.exists():
        with tempfile.TemporaryDirectory(prefix='.fixtures-', dir=DATA) as temporary:
            stage = pathlib.Path(temporary) / 'fixtures'
            rootfs = stage / 'rootfs'
            for name in ('bin', 'dev', 'proc', 'sys', 'tmp', 'mnt'):
                (rootfs / name).mkdir(parents=True)
            shutil.copyfile(shutil.which('busybox'), rootfs / 'bin/busybox')
            shutil.copyfile(ROOT / 'share/axern/runtime-selftest/memory-hog', rootfs / 'bin/memory-hog')
            for name in ('busybox', 'memory-hog'):
                (rootfs / 'bin' / name).chmod(0o755)
            for name in ('sh', 'sleep'):
                (rootfs / 'bin' / name).symlink_to('busybox')
            run('mkfs.erofs', str(stage / 'minimal.erofs'), str(ROOT / 'share/axern/erofs-root'), stdout=subprocess.DEVNULL)
            stage.rename(fixtures)
    safe_directory(OPT / 'runtime-selftest')
    link = OPT / 'runtime-selftest/rootfs'
    if not link.exists() and not link.is_symlink():
        link.symlink_to(fixtures / 'rootfs')
    elif not link.is_symlink() or link.readlink() != fixtures / 'rootfs':
        raise InstallError('unexpected existing runtime fixture path')


def install(settings: dict) -> None:
    receipt = read_json(ROOT / 'stage-receipt.json')
    checksum = receipt.get('archive_sha256', '')
    if not re.fullmatch('[0-9a-f]{64}', checksum) or ROOT != OPT / 'releases' / checksum:
        raise InstallError('stage the verified payload under /opt/axern/releases/<archive-sha256> first')
    manifest = read_json(ROOT / 'manifest.json')
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    if manifest['architecture'] != arch:
        raise InstallError('payload architecture does not match the host')
    existing = ETC / 'installation.json'
    identity = {'schema': 'axern.native-install.v1', 'archive_sha256': checksum, 'source_revision': manifest['source_revision'], 'settings': settings}
    first = not existing.exists()
    preflight(settings, first)
    if first:
        for directory in (ETC, DATA):
            if directory.exists() and (directory.is_symlink() or any(
                    entry.name != 'lost+found' or directory != DATA for entry in directory.iterdir())):
                raise InstallError(f'refusing to adopt existing data/configuration: {directory}')
        for name in (*[unit_name(service) for service in SERVICES], TARGET, 'axern.slice'):
            if (UNITS / name).exists() or (UNITS / name).is_symlink():
                raise InstallError(f'existing service unit is not owned by this installation: {name}')
            loaded = subprocess.run(['systemctl', 'show', '--property=LoadState,FragmentPath,ActiveState,Transient,DropInPaths', name], capture_output=True, text=True)
            properties = dict(line.split('=', 1) for line in loaded.stdout.splitlines() if '=' in line)
            implicit_slice = (name == 'axern.slice' and properties.get('LoadState') == 'loaded'
                              and properties.get('ActiveState') == 'inactive' and properties.get('Transient') == 'no'
                              and properties.get('FragmentPath') == '' and properties.get('DropInPaths') == '')
            if properties.get('LoadState') != 'not-found' and not implicit_slice:
                raise InstallError(f'service unit is already known to systemd or cannot be checked: {name}')
        for name in USERS:
            try:
                pwd.getpwnam('axern-' + name)
            except KeyError:
                continue
            raise InstallError(f'existing service account is not owned by this installation: axern-{name}')
        for name in ('gvisor', 'runtime-selftest'):
            if (OPT / name).exists() or (OPT / name).is_symlink():
                raise InstallError(f'existing runtime path is not owned by this installation: {name}')
        safe_directory(ETC)
        safe_directory(DATA)
        identity['data_identity'] = data_identity()
        write_file(existing, json_bytes(identity), 0o644)
    else:
        identity['data_identity'] = data_identity()
        if read_json(existing) != identity:
            raise InstallError('installation identity/configuration differs; upgrades and data adoption are not supported by this candidate')
    for name in USERS:
        account = 'axern-' + name
        try:
            entry = pwd.getpwnam(account)
            if entry.pw_shell != '/usr/sbin/nologin' or entry.pw_dir != '/nonexistent':
                raise InstallError(f'changed service account: {account}')
        except KeyError:
            run('useradd', '--system', '--user-group', '--home-dir', '/nonexistent', '--shell', '/usr/sbin/nologin', account)
    for directory in ('private', 'credentials'):
        safe_directory(ETC / directory, 0o700 if directory == 'private' else 0o755)
    for directory in ('axnoded', 'axnoded/root', 'axnoded/store', 'axnoded/rootfs', 'imagemgr', 'egressd'):
        safe_directory(DATA / directory)
    owned_data_directory(DATA / 'imagemgr/chunk_db', 'axern-imagefsd')
    secret_path = ETC / 'private/secrets.json'
    if not secret_path.exists():
        if (ETC / 'installed').exists():
            raise InstallError('installed identity material is missing; restore its backup')
        write_file(secret_path, json_bytes({'postgres_password': secrets.token_hex(32), 'master_key': base64.b64encode(secrets.token_bytes(32)).decode(), 'enrollment_token': secrets.token_hex(32)}))
    secret = read_json(secret_path)
    write_file(ETC / 'private/enrollment-token', (secret['enrollment_token'] + '\n').encode())
    credentials('postgres', 'password', (secret['postgres_password'] + '\n').encode())
    dsn = 'postgres://axern:' + secret['postgres_password'] + '@127.0.0.1:25432/axern?sslmode=disable'
    for role in ('control', 'retention'):
        environment = 'CONTROLD_POSTGRES_DSN=' + dsn + '\n'
        if role == 'control':
            environment += 'AXERN_SECRETS_MASTER_KEY=' + secret['master_key'] + '\n'
        else:
            environment += 'CONTROLD_RETENTION_ENABLED=true\n'
        credentials(role, 'environment', environment.encode())
    sync_identity()
    prepare_runtime()
    if not (DATA / 'postgres').exists():
        with tempfile.TemporaryDirectory(prefix='.postgres-', dir=DATA) as temporary:
            stage = pathlib.Path(temporary)
            owned_data_directory(stage, 'axern-postgres')
            run('runuser', '-u', 'axern-postgres', '--', str(PG / 'initdb'), '-D', str(stage / 'data'), '--username=axern', '--auth-host=scram-sha-256', '--auth-local=peer', '--pwfile=' + str(ETC / 'credentials/postgres/password'), stdout=subprocess.DEVNULL)
            (stage / 'data').rename(DATA / 'postgres')
    pg_settings = "listen_addresses = '127.0.0.1'\nport = 25432\nunix_socket_directories = '/run/axern-native-postgres'\n"
    write_file(ETC / 'postgres.conf', pg_settings.encode(), 0o644)
    write_file(ETC / 'node.toml', node_config(settings).encode())
    for name, text in service_units(ROOT, settings).items():
        write_file(UNITS / name, text.encode(), 0o644)
    write_file(ETC / 'client.env', (f'export AXERN_ENDPOINT=127.0.0.1:25000\nexport AXERN_TLS_CA_CERT={ETC}/ca.crt\nexport AXERN_TLS_CERT={ETC}/private/pki/client.crt\nexport AXERN_TLS_KEY={ETC}/private/pki/client.key\nexport AXERN_PROXY_MODE=direct\n').encode())
    write_file(ETC / 'installed', (checksum + '\n').encode(), 0o644)
    run('systemctl', 'daemon-reload')
    print('native installation prepared; start explicitly with the installed helper')


def database_bootstrap() -> None:
    if (ETC / 'private/database-bootstrapped').exists():
        return
    secret = read_json(ETC / 'private/secrets.json')
    environment = {**os.environ, 'PGPASSWORD': secret['postgres_password']}
    common = ['-h', '127.0.0.1', '-p', '25432', '-U', 'axern']
    exists = subprocess.check_output([str(PG / 'psql'), *common, '-d', 'postgres', '-tAc', "SELECT 1 FROM pg_database WHERE datname='axern'"], env=environment, text=True).strip()
    if exists != '1':
        run(str(PG / 'createdb'), *common, 'axern', env=environment)
    environment['CONTROLD_POSTGRES_DSN'] = 'postgres://axern:' + secret['postgres_password'] + '@127.0.0.1:25432/axern?sslmode=disable'
    run(str(ROOT / 'bin/controld-migrate'), 'up', env=environment)
    settings = read_json(ETC / 'installation.json')['settings']
    run(str(ROOT / 'bin/controld-access-bootstrap'), '-certificate', str(ETC / 'private/pki/client.crt'), '-node-id', settings['node_id'], '-enrollment-token-file', str(ETC / 'private/enrollment-token'), env=environment)
    write_file(ETC / 'private/database-bootstrapped', b'initialized\n')


def wait_ready(service: str) -> None:
    checks = {
        'postgres': [str(PG / 'pg_isready'), '-h', '127.0.0.1', '-p', '25432'],
        'imagefsd': [str(ROOT / 'bin/imagefsd'), 'stats-locality', '--chunk-db-dir', str(DATA / 'imagemgr/chunk_db'), '--chunk-server-sock', str(DATA / 'imagemgr/chunk_db/chunkserver.sock')],
        'imagemgr': ['curl', '--fail', '--silent', '--max-time', '2', '--unix-socket', '/run/axern-native-imagemgr/imagemgr.sock', 'http://unix/inventory'],
        'egressd': [str(ROOT / 'bin/egressdctl'), '-socket', '/run/axern-native-egressd/egressd.sock', 'health'],
        'axnoded': ['curl', '--fail', '--silent', '--max-time', '2', 'http://127.0.0.1:23001/readyz'],
    }
    if service not in checks:
        raise InstallError('select a service with an API readiness probe')
    deadline = time.monotonic() + 150
    while time.monotonic() < deadline:
        try:
            result = subprocess.run(checks[service], timeout=3, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if result.returncode == 0:
                return
        except subprocess.TimeoutExpired:
            pass
        time.sleep(0.5)
    raise InstallError(f'{service} failed its API readiness probe; inspect journalctl -u {unit_name(service)}')


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('install', 'start', 'stop', 'status', 'renew-services', 'internal-bootstrap', 'internal-ready', 'internal-verify'))
    parser.add_argument('service', nargs='?', choices=SERVICES)
    parser.add_argument('--config', type=pathlib.Path)
    args = parser.parse_args()
    try:
        if args.command == 'internal-ready':
            wait_ready(args.service)
            return
        if args.command == 'internal-verify':
            verify_installed()
            return
        if os.geteuid() != 0:
            raise InstallError('installation and lifecycle operations require root')
        os.umask(0o077)
        if args.command == 'internal-bootstrap':
            database_bootstrap()
            return
        descriptor = os.open('/run/lock/axern-native-install.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, 'w') as lock:
            info = os.fstat(lock.fileno())
            if info.st_uid != 0 or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise InstallError('installer lock is not a root-owned regular file')
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if args.command == 'install':
                if args.config is None:
                    raise InstallError('--config is required for installation')
                install(validate_settings(read_json(args.config)))
                return
            identity = verify_installed(check_data=args.command not in ('stop', 'status'))
            if args.command == 'start':
                preflight(validate_settings(identity['settings']), False)
                run('systemctl', 'enable', '--now', TARGET)
                run('systemctl', 'is-active', *[unit_name(name) for name in SERVICES])
                wait_ready('axnoded')
                print('native services started; run a real sandbox to qualify serving')
            elif args.command == 'stop':
                run('systemctl', 'stop', TARGET)
                print('native services stopped; configuration and data retained')
            elif args.command == 'renew-services':
                sync_identity(renew=True)
                run('systemctl', 'try-restart', *[unit_name(name) for name in ('controld', 'tunneld', 'gatewayd')])
            else:
                run('systemctl', '--no-pager', 'status', TARGET, *[unit_name(name) for name in SERVICES])
    except (InstallError, OSError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
        parser.exit(1, f'native-install: {error}\n')


if __name__ == '__main__':
    main()
