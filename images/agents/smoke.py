"""Check built image identity, tools and shared layers without model access."""

import json
import os
import subprocess
import time

from build import arguments, docker_command, image_tags, selected_targets
from install import load_lock


SCRIPT = r'''
import json,os,pathlib,shutil,subprocess,sys,tempfile
lock=json.loads(pathlib.Path('/usr/local/share/axern/agent-images/versions.lock.json').read_text())
assert os.getuid()==1000
assert pathlib.Path.cwd()==pathlib.Path('/workspace')
assert tuple(map(int,lock['base']['python_version'].split('.')))==sys.version_info[:3]
release=dict(line.split('=',1) for line in pathlib.Path('/etc/os-release').read_text().splitlines() if '=' in line)
assert release['ID'].strip('"')=='debian'
assert release['VERSION_ID'].strip('"')==lock['base']['debian_version']
pip_version=subprocess.check_output([sys.executable,'-m','pip','--version'],text=True).split()[1]
assert pip_version==lock['artifacts']['pip']['version']
for command in (['git','--version'],['rg','--version']):subprocess.run(command,check=True,stdout=subprocess.DEVNULL)
stage=sys.argv[1]
observed={'python':'.'.join(map(str,sys.version_info[:3])),'debian':release['VERSION_ID'].strip('"'),'pip':pip_version,'uid':os.getuid()}
if stage!='base':
 for name in ('npm','npx','corepack','pnpm','pnpx','yarn','yarnpkg'):
  assert shutil.which(name) is None, name+' must be supplied by a caller-owned project image'
  path=pathlib.Path('/opt/node/bin')/name
  assert not path.exists() and not path.is_symlink()
 managers=pathlib.Path('/opt/node/lib/node_modules')
 assert not managers.exists() and not managers.is_symlink()
 observed['node_package_managers']='caller-owned; not installed'
 for name,command in (('go',['go','version']),('node',['node','--version'])):
  value=subprocess.check_output(command,text=True).strip()
  assert lock['artifacts'][name]['version'] in value
  observed[name]=value
 with tempfile.TemporaryDirectory() as temporary:
  root=pathlib.Path(temporary)
  (root/'main.go').write_text('package main\nfunc main() {}\n')
  subprocess.run(['go','build','-o',str(root/'hello'),str(root/'main.go')],check=True,env={**os.environ,'GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off'})
  subprocess.run([str(root/'hello')],check=True)
 subprocess.run(['node','-e','if (6*7!==42) process.exit(1)'],check=True)
 subprocess.run(['cc','--version'],check=True,stdout=subprocess.DEVNULL)
if stage in ('claude-code','codex'):
 program='claude' if stage=='claude-code' else 'codex'
 value=subprocess.check_output([program,'--version'],text=True).strip()
 assert lock['artifacts'][stage]['version'] in value
 subprocess.run([program,'--help'],check=True,stdout=subprocess.DEVNULL)
 observed[program]=value
 if stage=='codex':
  vendor=pathlib.Path('/opt/codex/vendor/x86_64-unknown-linux-musl')
  assert (vendor/'codex-resources/bwrap').is_file()
  assert (vendor/'codex-path/rg').is_file()
  assert (vendor/'codex-resources/voice/NOTICE.md').is_file()
 else:
  assert pathlib.Path('/opt/claude-code/node_modules/@anthropic-ai/claude-code/LICENSE.md').is_file()
print(json.dumps(observed))
'''


PRIVILEGE_SCAN = r'''
import json,os,stat
special=[]
errors=[]
count=0
for root,dirs,files in os.walk('/',onerror=lambda error:errors.append(str(error))):
 dirs[:]=[name for name in dirs if os.path.join(root,name) not in ('/proc','/sys','/dev')]
 for name in files:
  path=os.path.join(root,name)
  try:info=os.stat(path,follow_symlinks=False)
  except OSError as error:errors.append(str(error));continue
  if not stat.S_ISREG(info.st_mode):continue
  count+=1
  if info.st_mode&(stat.S_ISUID|stat.S_ISGID):special.append({'path':path,'mode':oct(stat.S_IMODE(info.st_mode))})
print(json.dumps({'regular_files_checked':count,'suid_sgid_files':special,'errors':errors,'excluded_virtual_trees':['/proc','/sys','/dev']}))
if special or errors:raise SystemExit('image privilege-bit scan failed')
'''


def main():
    args = arguments(__doc__, cache=False).parse_args()
    lock = load_lock()
    targets = selected_targets(args.targets)
    tags = image_tags(lock, args.tag_prefix, args.tag_suffix)
    output = args.output_dir.resolve() if args.output_dir else None
    if output:
        output.mkdir(parents=True, exist_ok=True)
    env = {key: value for key, value in os.environ.items() if not any(part in key.upper() for part in ('DEEPSEEK', 'ANTHROPIC', 'OPENAI', 'CLAUDE', 'CODEX_API_KEY'))}
    reports, layers = [], {}
    for target in targets:
        prefix = docker_command(args.docker_config)
        image = json.loads(subprocess.check_output(prefix + ['image', 'inspect', tags[target]], env=env))[0]
        if image['Config']['User'] != '1000:1000':
            raise SystemExit('Agent image must default to UID/GID 1000')
        layers[target] = image['RootFS']['Layers']
        command = prefix + ['run', '--rm', '--network', 'none', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '-i', tags[target], 'python3', '-', target]
        started = time.monotonic()
        process = subprocess.run(command, input=SCRIPT.encode(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, timeout=180)
        report = {'target': target, 'image': tags[target], 'exit_code': process.returncode, 'elapsed_seconds': round(time.monotonic() - started, 3), 'stdout': process.stdout.decode(), 'passed': process.returncode == 0}
        reports.append(report)
        if output:
            (output / (target + '-smoke.log')).write_bytes(process.stdout)
            (output / 'smoke-receipts.json').write_text(json.dumps(reports, indent=2) + '\n')
        if process.returncode:
            raise SystemExit(process.stdout.decode())
        scan_command = prefix + ['run', '--rm', '--network', 'none', '--cap-drop', 'ALL', '--cap-add', 'DAC_OVERRIDE', '--security-opt', 'no-new-privileges', '--user', '0:0', '-i', tags[target], 'python3', '-']
        scan_started = time.monotonic()
        scan = subprocess.run(scan_command, input=PRIVILEGE_SCAN.encode(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, timeout=180)
        report['privilege_scan'] = {'command': scan_command, 'exit_code': scan.returncode, 'elapsed_seconds': round(time.monotonic() - scan_started, 3), 'stdout': scan.stdout.decode()}
        report['passed'] = scan.returncode == 0
        if output:
            (output / (target + '-privilege-scan.log')).write_bytes(scan.stdout)
            (output / 'smoke-receipts.json').write_text(json.dumps(reports, indent=2) + '\n')
        if scan.returncode:
            raise SystemExit(scan.stdout.decode())
    if 'toolchain' in layers:
        for target in ('claude-code', 'codex'):
            if target in layers and layers[target][:len(layers['toolchain'])] != layers['toolchain']:
                raise SystemExit('Agent image does not reuse the complete toolchain layers')
    print(json.dumps({'passed': True, 'checks': reports, 'shared_toolchain_layers': 'toolchain' in layers}, indent=2))


if __name__ == '__main__':
    main()
