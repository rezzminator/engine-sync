# professor compiler fixture

Produced by the base 0.1.0 binary. Its machine file is a copy of Professor's shipped machine policy and its project file the repository-only delta, the split Professor ships; it exercises project, child, workbench, MCP and machine variants. Golden bytes are fixed except for normalized marker/rebuild-hint lines.

`input/project` and `input/home` are copied into a test jail. `golden/artifacts.json` records all generated surfaces, including link targets and file modes. Corresponding absolute roots become `{project}` and `{home}`; marker lines become `{marker}`. Regenerate with `scripts/golden-projects.py --binary PATH --fixture professor`; the Professor producer is the prescribed base binary, selected with `--base`.
