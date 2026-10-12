package machine

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"slices"
)

// skipBrokenAgents moves each agent whose Codex role or OpenCode agent does
// not render or validate out of the model and into Failed, naming why in
// Problems: one broken agent is skipped with its installed copies kept, and
// every other agent still installs. An agent Failed already names (the plan
// pass's skip, carried into the build pass's source) is dropped unnamed: one
// run names each broken agent once. An unmet requires is filterMachineAgents'
// to retire, never this.
func skipBrokenAgents(model source.Project, cfg *config.Config) source.Project {
	agents := make([]source.Agent, 0, len(model.Agents))
	model.Problems, model.Failed = slices.Clip(model.Problems), slices.Clip(model.Failed)
	for _, agent := range model.Agents {
		if slices.Contains(model.Failed, agentName(agent)) {
			continue
		}
		if problems := agentProblems(model, agent, cfg); len(problems) > 0 {
			model.Problems = append(model.Problems, problems...)
			model.Failed = append(model.Failed, agentName(agent))
			continue
		}
		agents = append(agents, agent)
	}
	model.Agents = agents
	return model
}

// agentProblems renders one agent alone for both engines and returns every
// error its Codex role or OpenCode agent reports.
func agentProblems(model source.Project, agent source.Agent, cfg *config.Config) []string {
	_, content, err := codex.RenderMachineAgent(agent.Path, string(agent.Raw), model.AgentSourceDir, *cfg)
	if err != nil {
		return []string{err.Error()}
	}
	if err := codex.ValidateRole(content); err != nil {
		return []string{fmt.Sprintf("%s: does not parse: %v", agent.Path, err)}
	}
	single := model
	single.Agents, single.Failed, single.Problems, single.Warnings, single.MachineCommands = []source.Agent{agent}, nil, nil, nil, false
	target := opencode.Target{}
	artifacts, diagnostics := target.Render(single, *cfg)
	var problems []string
	for _, d := range append(diagnostics, target.Validate(artifacts)...) {
		if d.Severity != diag.Warning {
			problems = append(problems, d.Message)
		}
	}
	return problems
}
