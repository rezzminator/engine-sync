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
 put('scripts/baselines/registry-parity.txt','');put('docs/architecture.md','## Package responsibilities\n\n| Package | Responsibility |\n| --- | --- |\n| cli | Commands |\n');put('internal/cli/main.go','package cli\nvar Version = "0.1.0"\nvar table=[]entry{{"build",run}}\n');put('plugins/engine-sync/.claude-plugin/plugin.json',json.dumps({'version':'0.1.0'}));put('README.md','## Commands\n| `build` | Build |\n');put('docs/interface.md','## Commands\n| `build` | Build |\n');put('docs/engine-sync.schema.json','{}\n');p=put('schema','#!/bin/sh\nprintf "{}\\n"\n');p.chmod(0o755);r=run('registry-parity');assert r.returncode==1 and 'version' in r.stdout.lower(),('equal release version certified',r.stdout,r.stderr)
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
# Private terms not supplied is a GAP (exit 3): neither a pass nor a red. A supplied file that is unusable stays an ERROR.
ids=[f.stem for f in script.with_name('baselines').glob('*.txt')]
with tempfile.TemporaryDirectory(dir=os.environ.get('TMPDIR', '/tmp')) as tmp2:
 from tree_test_fixture import git_enumerator
 git_enumerator(tmp2)
 root=pathlib.Path(tmp2)
 for name in ids:put('scripts/baselines/'+name+'.txt','')
 put('internal/fixture/doc.go','// Package fixture seeds a complete enumeration.\npackage fixture\n')
 put('cmd/engine-sync/main.go','// Package main is the fixture command.\npackage main\n');put('cmd/engine-sync/main_test.go','package main\n')
 put('internal/cli/main.go','// Package cli runs the fixture.\npackage cli\nvar Version = "1.1.0-alpha"\nvar commands = []entry{{"build", run}}\n');put('internal/cli/main_test.go','package cli\n')
 put('plugins/engine-sync/.claude-plugin/plugin.json',json.dumps({'version':'1.0.0'}))
 put('README.md','## Commands\n\n| `build` | Build |\n');put('docs/interface.md','## Commands\n\n| `build` | Build |\n');put('docs/engine-sync.schema.json','{}\n');put('docs/architecture.md','## Package responsibilities\n\n| Package | Responsibility |\n| --- | --- |\n| cli, fixture | Fixture packages |\n')
 schema=put('schema','#!/bin/sh\nprintf "{}\\n"\n');schema.chmod(0o755)
 bare={k:v for k,v in os.environ.items() if k!='LEAK_TERMS'}
 def tree(*args,env=bare):return subprocess.run(['python3',str(script),'--root',str(root),'--binary',str(schema),*args],env=env,capture_output=True,text=True)
 r=tree('--only','publication');assert r.returncode==3 and 'CHECK publication GAP LEAK_TERMS not supplied' in r.stdout and 'CHECK publication PASS' not in r.stdout and 'CHECK publication ERROR' not in r.stdout,('unsupplied terms not a gap',r.returncode,r.stdout,r.stderr)
 r=tree();passed=[line for line in r.stdout.splitlines() if line.endswith(' PASS')]
 ran=len(ids)-1;assert r.returncode==3 and len(passed)==ran and 'CHECK publication GAP' in r.stdout and f'CHECK TREE GAP — not run: publication · ran {ran}: {ran} PASS, 0 FAIL, 0 ERROR' in r.stdout,('full run without terms',r.returncode,r.stdout,r.stderr)
 r=tree(env=dict(bare,LEAK_TERMS=''));assert r.returncode==3 and 'CHECK publication GAP' in r.stdout,('empty LEAK_TERMS not unsupplied',r.returncode,r.stdout,r.stderr)
 r=tree('--only','publication',env=dict(bare,LEAK_TERMS=str(root/'absent-terms')));assert r.returncode==2 and 'CHECK publication ERROR LEAK_TERMS file required' in r.stdout and 'GAP' not in r.stdout,('wrong LEAK_TERMS path hidden as a gap',r.returncode,r.stdout,r.stderr)
 put('internal/unit/unit.go','package unit\n');put('internal/unit/unit_test.go','package unit\n')
 r=tree();assert r.returncode==1 and 'CHECK doc-go FAIL' in r.stdout and 'CHECK publication GAP' in r.stdout,('a gap masked a red check',r.returncode,r.stdout,r.stderr)
 (root/'internal/unit/unit.go').unlink();(root/'internal/unit/unit_test.go').unlink()
 put('scripts/leak-terms.txt','fixture-private-token\n');put('scripts/leak-check.sh','echo "leak-check: clean: 3 files, 1 terms"\n')
 r=tree('--only','publication');assert r.returncode==0 and 'CHECK publication PASS leak-check: clean: 3 files, 1 terms' in r.stdout,('default terms file unused',r.returncode,r.stdout,r.stderr)
 # A gap under --measure leaves its baseline as it was: no scan ran to measure.
 (root/'scripts/leak-terms.txt').unlink();put('scripts/baselines/publication.txt','kept-row\n')
 r=tree('--only','publication','--measure');assert r.returncode==3 and (root/'scripts/baselines/publication.txt').read_text()=='kept-row\n',('measure rewrote a baseline no scan measured',r.returncode,r.stdout,r.stderr)
 # A check that ended in ERROR ran: the footer never also lists it as not run.
 (root/'scripts/baselines/publication.txt').unlink()
 r=tree('--only','publication');assert r.returncode==2 and 'CHECK publication ERROR baseline missing' in r.stdout and 'not run: publication' not in r.stdout,('a check reported both ran and not run',r.returncode,r.stdout,r.stderr)
 put('scripts/baselines/publication.txt','')
 # The default terms file that cannot be probed is a publication ERROR, never a traceback.
 probe="import pathlib,runpy,sys\nreal=pathlib.Path.is_file\ndef is_file(self):\n if self.name=='leak-terms.txt':raise PermissionError(13,'Permission denied',str(self))\n return real(self)\npathlib.Path.is_file=is_file\nsys.argv=sys.argv[1:]\nrunpy.run_path(sys.argv[0],run_name='__main__')\n"
 r=subprocess.run(['python3','-c',probe,str(script),'--root',str(root),'--binary',str(schema),'--only','publication'],env=bare,capture_output=True,text=True)
 assert r.returncode==2 and 'CHECK publication ERROR' in r.stdout and 'Traceback' not in r.stderr,('an unprobeable default terms file escaped as a traceback',r.returncode,r.stdout,r.stderr)
print('GATE REGRESSIONS PASS — empty source/registry, version, standalone surface, malformed measure, UTF-8, publication input, unsupplied terms gap, gap under measure, gap with error, unprobeable default')
