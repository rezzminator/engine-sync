#!/usr/bin/env python3
"""Capture a fixture whose producer writes a file that is not text."""
import json, os, pathlib, shutil, subprocess, sys, tempfile
script=pathlib.Path(__file__).with_name('golden-projects.py').resolve()
base=pathlib.Path(os.environ.get('TMPDIR','/tmp'));base.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='golden-tests-',dir=base) as tmp:
 tree=pathlib.Path(tmp)
 # The script finds its fixture beside itself, so a copy captures into this tree, never testdata/.
 (tree/'scripts').mkdir();shutil.copy(script,tree/'scripts'/script.name)
 for surface in ['project','home']:(tree/'testdata/projects/plain/input'/surface).mkdir(parents=True)
 producer=tree/'producer'
 producer.write_text('#!/bin/sh\n[ "$1" = global ] || exit 0\nprintf \'\\377\\000\' > "$HOME/blob.bin" && chmod 600 "$HOME/blob.bin"\n')
 producer.chmod(0o755)
 done=subprocess.run([sys.executable,str(tree/'scripts'/script.name),'--binary',str(producer),'--fixture','plain'],text=True,capture_output=True)
 golden=tree/'testdata/projects/plain/golden/artifacts.json'
 assert done.returncode==0,('capture failed',done.stdout,done.stderr)
 got=json.loads(golden.read_text())
 want={'home/blob.bin':{'kind':'bytes','content':'/wA=','mode':0o600}}
 assert got==want,(str(golden),got,want)
print('GOLDEN CAPTURE TESTS PASS — a file that is not text is recorded as its bytes')
