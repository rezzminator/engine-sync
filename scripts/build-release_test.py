import os,pathlib,subprocess,tempfile,sys,json
producer=pathlib.Path(sys.argv[1] if len(sys.argv)>1 else pathlib.Path(__file__).with_name('build-release.sh')).resolve()
with tempfile.TemporaryDirectory(dir=os.environ.get('TMPDIR', '/tmp')) as tmp:
 root=pathlib.Path(tmp);scripts=root/'scripts';scripts.mkdir();(scripts/'build-release.sh').write_bytes(producer.read_bytes())
 manifest=root/'plugins/engine-sync/.claude-plugin';manifest.mkdir(parents=True);(manifest/'plugin.json').write_text(json.dumps({'version':'0.1.0'}))
 tools=root/'bin';tools.mkdir();go=tools/'go';go.write_text("""#!/bin/sh
case "$1:$2" in
 env:GOVERSION) echo go1.27.1;;
 env:GOHOSTOS) echo darwin;;
 env:GOHOSTARCH) echo arm64;;
 version:-m) echo 'build -ldflags=-X github.com/rezzminator/engine-sync/internal/cli.Version=0.1.0';;
 build:*) while [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then shift; asset=$1; break; fi; shift; done
 printf '#!/bin/sh\necho engine-sync 0.0.0\n' > "$asset";chmod +x "$asset";;
 *) exit 1;;
esac
""");go.chmod(0o755)
 env=dict(os.environ,PATH=str(tools)+os.pathsep+os.environ['PATH'])
 result=subprocess.run(['bash',str(scripts/'build-release.sh'),'--out',str(root/'out')],env=env,capture_output=True,text=True)
 assert result.returncode!=0 and 'reports engine-sync 0.0.0' in result.stderr,('misstamped release accepted',result.returncode,result.stdout,result.stderr)
print('RELEASE VERSION REGRESSION PASS')
