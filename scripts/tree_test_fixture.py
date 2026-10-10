"""Read-only Git command double for synthetic, non-repository gate fixtures."""
import os, pathlib, sys

def git_enumerator(root):
    binary=pathlib.Path(root)/'tools'/'git'
    binary.parent.mkdir(exist_ok=True)
    binary.write_text('#!'+sys.executable+'\n'+'''import pathlib,sys
if sys.argv[1:] != ['ls-files','-co','--exclude-standard','-z']: raise SystemExit(1)
for path in sorted(pathlib.Path('.').rglob('*')):
 if path.is_file() and 'tools' not in path.parts and path.name!='ignored.go':
  sys.stdout.buffer.write(str(path).encode()+b'\\0')
''')
    binary.chmod(0o755)
    os.environ['PATH']=str(binary.parent)+os.pathsep+os.environ['PATH']
