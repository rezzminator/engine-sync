# Integration fixture

The machine file copies the shipped integration policy and the project file supplies the repository-only delta. The fixture exercises project, child, workbench, MCP and machine source declarations.

`input/project` and `input/home` are copied into a test jail. `golden/artifacts.json` records generated surfaces, link targets and file modes. Absolute roots become `{project}` and `{home}`; generated marker lines are retained. Regenerate with the current binary using `scripts/golden-projects.py --binary PATH --fixture integration`, without `--base`. Review the intended-only artifact diff.
