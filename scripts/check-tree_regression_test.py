import pathlib,tempfile,subprocess,json,os
script=pathlib.Path(__file__).with_name('check-tree.py').resolve()
with tempfile.TemporaryDirectory(dir=os.environ.get('TMPDIR', '/tmp')) as tmp:
 from tree_test_fixture import git_enumerator
 git_enumerator(tmp)
 root=pathlib.Path(tmp)
 def put(name,text):
  p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text);return p
 def run(check):return subprocess.run(['python3',str(script),'--root',str(root),'--only',check,'--binary',str(root/'schema')],capture_output=True,text=True)
 put('scripts/baselines/size-src.txt','');r=run('size-src');assert r.returncode==2 and 'ERROR' in r.stdout,('empty tree certified',r.stdout,r.stderr)
 put('scripts/baselines/registry-parity.txt','');put('internal/cli/main.go','package cli\nvar Version = "0.1.0"\nvar table=[]entry{{"build",run}}\n');put('plugins/engine-sync/.claude-plugin/plugin.json',json.dumps({'version':'0.1.0'}));put('README.md','## Commands\n| `build` | Build |\n');put('docs/interface.md','## Commands\n| `build` | Build |\n');put('docs/engine-sync.schema.json','{}\n');p=put('schema','#!/bin/sh\nprintf "{}\\n"\n');p.chmod(0o755);r=run('registry-parity');assert r.returncode==1 and 'version' in r.stdout.lower(),('equal release version certified',r.stdout,r.stderr)
 put('internal/cli/main.go','package cli\nvar Version = "0.2.0-alpha"\nvar table=[]entry{{"build",run}}\n')
 put('plugins/engine-sync/.claude-plugin/plugin.json',json.dumps({'version':'0.2.0'}));r=run('registry-parity');assert r.returncode==1,('prerelease core equals release',r.stdout,r.stderr)
 put('plugins/engine-sync/.claude-plugin/plugin.json',json.dumps({'version':'0.1.0'}));r=run('registry-parity');assert r.returncode==0,('newer prerelease refused',r.stdout,r.stderr)
 put('scripts/baselines/spelling-professor.txt','');put('plugins/engine-sync/libexec/engine-sync','#!/bin/sh\n# First action: read the repository root AGENTS.md in full.\n');r=run('spelling-professor');assert r.returncode==1,('wrapper policy unseen',r.stdout,r.stderr)
 # A malformed producer never rewrites a measured baseline.
 put('scripts/baselines/size-src.txt','bad-number\n');before=(root/'scripts/baselines/size-src.txt').read_bytes()
 r=subprocess.run(['python3',str(script),'--root',str(root),'--only','size-src','--measure'],capture_output=True,text=True)
 assert r.returncode==2 and 'ERROR' in r.stdout and (root/'scripts/baselines/size-src.txt').read_bytes()==before,(r.stdout,r.stderr)
 put('scripts/baselines/size-src.txt','');(root/'internal/cli/main.go').write_bytes(b'package cli\n\xff')
 r=run('size-src');assert r.returncode==2 and 'ERROR' in r.stdout,(r.stdout,r.stderr)
 put('internal/cli/main.go','package cli\nvar Version = "0.2.0-alpha"\n')
 r=run('registry-parity');assert r.returncode==2 and 'empty command enumeration' in r.stdout,(r.stdout,r.stderr)
 put('scripts/baselines/publication.txt','');terms=put('terms','');env=dict(os.environ,LEAK_TERMS=str(terms))
 r=subprocess.run(['python3',str(script),'--root',str(root),'--only','publication'],env=env,capture_output=True,text=True)
 assert r.returncode==2 and 'zero usable terms' in r.stdout,(r.stdout,r.stderr)
print('GATE REGRESSIONS PASS — empty source/registry, version, standalone surface, malformed measure, UTF-8, publication input')
