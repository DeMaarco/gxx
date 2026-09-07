// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package session assembles the same agent core for the CLI and evaluations.
package session

import (
	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/config"
	"gxx/internal/skills"
	"gxx/internal/tools"
	"gxx/internal/workspace"
)

type Options struct {
	Backend       agent.Backend // Optional injection for deterministic tests.
	IsolateSkills bool
	Mode          string
	Eco           int
}

type Session struct {
	Backend  agent.Backend
	Registry *tools.Registry
	Loop     *agent.Loop
}

// New does not own ws; callers close it after the session finishes.
func New(settings config.Config, ws *workspace.Workspace, approver approval.Approver, options Options) (*Session, error) {
	backend := options.Backend
	if backend == nil {
		var err error
		backend, err = NewBackend(settings, ws)
		if err != nil {
			return nil, err
		}
	}
	state := config.ApplyEco(settings, options.Eco)
	registry := tools.NewRegistry(ws, approver, tools.Options{
		MaxResultBytes: state.MaxToolResultBytes, MaxSearchResult: settings.MaxSearchResults,
		ParallelReads: settings.ParallelReads, CommandTimeout: settings.CommandTimeout,
	})
	catalog := func() []skills.Skill {
		personal := ""
		if !options.IsolateSkills {
			personal, _ = config.UserSkillsDir()
		}
		return skills.Discover(ws, personal)
	}
	registry.SetSkillsCatalog(catalog)
	registry.SetAsk(options.Mode == "ask")
	registry.SetPlan(options.Mode == "plan")
	backend.SetModel(settings.Model)
	backend.SetEffort(settings.Effort)
	backend.SetContext(settings.Context)
	backend.SetFast(settings.Fast)
	backend.SetTokenBudget(state.Level, state.CompactNumer, state.CompactDenom, state.ToolOutputKeep, state.ToolOutputClip, state.IncludeReasoning)
	backend.SetInstructions(agent.SystemPromptForSkills(ws, registry.Plan(), registry.Ask(), state.Level, catalog()))
	loop := &agent.Loop{
		Model: backend, Executor: registry, MaxSteps: settings.MaxSteps, Overview: registry.WorkspaceOverview,
		ProjectContext: func() string { return agent.ProjectContext(ws, state.Level) },
		SkillsContext:  func() string { return agent.SkillsContextForCatalog(catalog()) },
	}
	return &Session{Backend: backend, Registry: registry, Loop: loop}, nil
}
