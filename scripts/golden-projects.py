#!/usr/bin/env python3
"""Capture compiler-owned files and links using an explicitly selected producer."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', type=Path, required=True)
parser.add_argument('--fixture', choices=['plain', 'integration'], required=True)
args = parser.parse_args()
fixture = Path(__file__).resolve().parent.parent / 'testdata/projects' / args.fixture
scratch = Path(os.environ.get('TMPDIR','/tmp'))
scratch.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='golden-', dir=scratch) as temporary:
    jail = Path(temporary)
    shutil.copytree(fixture / 'input', jail, dirs_exist_ok=True)
    root, home = jail / 'project', jail / 'home'
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('ENGINE_SYNC_', 'XDG_', 'CLAUDE_', 'CODEX_'))}
    env.update(HOME=str(home), TMPDIR=str(scratch),
               XDG_STATE_HOME=str(jail / 'state'), XDG_DATA_HOME=str(home / '.local/share'),
               CLAUDE_PROJECT_DIR=str(root), GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_NOSYSTEM='1')
    commands = [['build', str(root)], ['check', str(root)], ['global']]
    if args.fixture == 'integration':
        commands[-1] += ['--source-repo', str(root)]
    for command in commands:
        result = subprocess.run([str(args.binary.resolve()), *command], env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if result.returncode:
            raise SystemExit(result.stdout.decode() + result.stderr.decode())
    artifacts = {}
    for label, surface in [('project', root), ('home', home)]:
        for path in sorted(surface.rglob('*')):
            name = path.relative_to(surface).as_posix()
            include = label == 'project' and (path.name == 'AGENTS.md' or
                      name.startswith(('.codex/', '.opencode/')))
            include |= label == 'home'
            if not include or path.is_dir() and not path.is_symlink():
                continue
            def normalize(text):
                return text.replace(str(root), '{project}').replace(str(home), '{home}')
            if path.is_symlink():
                artifact = {'kind': 'link', 'target': normalize(os.readlink(path))}
            else:
                lines = normalize(path.read_text()).splitlines(keepends=True)
                artifact = {'kind': 'file', 'content': ''.join(lines), 'mode': path.stat().st_mode & 0o777}
            artifacts[label + '/' + name] = artifact
    output = fixture / 'golden/artifacts.json'
    output.parent.mkdir(exist_ok=True)
    output.write_text(json.dumps(artifacts, indent=2, sort_keys=True, ensure_ascii=False) + '\n')
    print(f'{args.fixture}: {len(artifacts)} artifacts; producer {args.binary.name}')
