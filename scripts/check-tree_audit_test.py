"""Exercise every audit plant against the real gate, with a fixture enumerator."""
import os, pathlib, subprocess, tempfile
script=pathlib.Path(__file__).with_name('check-tree.py').resolve()
with tempfile.TemporaryDirectory(dir=os.environ['TMPDIR']) as tmp:
 from tree_test_fixture import git_enumerator
 git_enumerator(tmp)
 root=pathlib.Path(tmp)
 def put(name,text):
  p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text);return p
 for check in ['spelling-professor','facade-exec','facade-env','facade-frontmatter','size-cmd','orientation-truth']:
  put('scripts/baselines/'+check+'.txt','')
 put('internal/seed/seed.go','// Package seed seeds the test.\npackage seed\n')
 cases=[('spelling-professor','README.md','professor'),('spelling-professor','docs/interface.md','workbench gitter'),('spelling-professor','CLAUDE.md','pfm'),('spelling-professor','internal/seed/seed_test.go','package seed\n// fleet dispatch prompt'),('spelling-professor','plugins/engine-sync/libexec/engine-sync','flight framework workbench.json'),('facade-exec','internal/seed/plant.go','package seed\nfunc run(){exec.CommandContext(ctx,"git")}'),('facade-env','internal/seed/plant.go','package seed\nfunc run(){os.Environ()}'),('facade-env','internal/config/plant.go','package config\nfunc run(){os.ExpandEnv("HOME");os.Setenv("x","y")}'),('facade-frontmatter','internal/seed/plant.go','package seed\nfunc decodeFrontmatter(text string){}'),('size-cmd','internal/cli/plant.go','package cli\n'+'\n'*900),('orientation-truth','docs/interface.md','```text\nplugins/absent/file.go\n```\n'),('orientation-truth','CLAUDE.md','Read `.github/absent.yml`.\n')]
 failures=[]
 for check,name,body in cases:
  p=put(name,body)
  r=subprocess.run(['python3',str(script),'--root',str(root),'--only',check],text=True,capture_output=True)
  if r.returncode!=1 or f'CHECK {check} FAIL' not in r.stdout: failures.append((check,name,r.returncode,r.stdout,r.stderr))
  p.unlink()
 put('internal/cli/ignored.go','package cli\n'+'\n'*901)
 r=subprocess.run(['python3',str(script),'--root',str(root),'--only','size-cmd'],text=True,capture_output=True)
 if r.returncode!=0:failures.append(('ignored entry scanned',r.stdout,r.stderr))
 if failures:
  for row in failures:print('MISSED',row)
  raise SystemExit(1)
 print('AUDIT GATE PLANTS PASS')
