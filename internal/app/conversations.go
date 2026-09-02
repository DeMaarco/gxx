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

package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gxx/internal/conversations"
	"gxx/internal/config"
	"gxx/internal/ui"
)

func (rt *runtime) listConversationEntries() ([]ui.ConversationEntry, error) {
	if rt == nil || rt.conversationStore == nil {
		return nil, nil
	}
	records, err := rt.conversationStore.List(rt.config.Workspace, rt.config.Provider)
	if err != nil {
		return nil, err
	}
	entries := make([]ui.ConversationEntry, 0, len(records))
	for _, record := range records {
		title := conversations.TitleFromHistory(record.Provider, record.History)
		entries = append(entries, ui.ConversationEntry{
			ID:        record.ID,
			Title:     title,
			Model:     record.Model,
			UpdatedAt: record.UpdatedAt,
		})
	}
	return entries, nil
}

func (rt *runtime) saveConversation() error {
	if rt == nil || rt.conversationStore == nil || rt.provider == nil {
		return nil
	}
	provider, history, err := rt.provider.ExportHistory()
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return nil
	}
	now := time.Now().UTC()
	record := conversations.Record{
		ID:        rt.activeConversationID,
		Title:     conversations.TitleFromHistory(provider, history),
		Workspace: rt.config.Workspace,
		Provider:  provider,
		Model:     rt.config.Model,
		Effort:    rt.config.Effort,
		Context:   rt.config.Context,
		UpdatedAt: now,
		History:   history,
	}
	if strings.TrimSpace(record.ID) == "" {
		record.ID = conversations.NewID()
		record.CreatedAt = now
		rt.activeConversationID = record.ID
	} else if existing, err := rt.conversationStore.Load(record.ID); err == nil {
		record.CreatedAt = existing.CreatedAt
	} else {
		record.CreatedAt = now
	}
	return rt.conversationStore.Save(record)
}

func (rt *runtime) loadConversation(id string) error {
	if rt == nil || rt.conversationStore == nil || rt.provider == nil {
		return fmt.Errorf("conversation store unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("conversation id cannot be empty")
	}
	if err := rt.saveConversation(); err != nil {
		return err
	}
	record, err := rt.conversationStore.Load(id)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(record.Workspace), filepath.Clean(rt.config.Workspace)) {
		return fmt.Errorf("conversation belongs to another workspace")
	}
	if record.Provider != rt.config.Provider {
		return fmt.Errorf("conversation uses %s; switch to a %s model first", record.Provider, record.Provider)
	}
	if err := rt.provider.ImportHistory(record.Provider, record.History); err != nil {
		return err
	}
	rt.config.Model = record.Model
	rt.config.Effort = record.Effort
	rt.config.Context = record.Context
	rt.config.Provider = config.ProviderForModel(record.Model)
	rt.provider.SetModel(record.Model)
	rt.provider.SetEffort(record.Effort)
	rt.provider.SetContext(record.Context)
	rt.provider.SetInstructions(rt.systemPrompt())
	if _, err := config.SaveSession(
		record.Model,
		record.Effort,
		record.Context,
		rt.config.Fast,
		rt.config.PermissionMode,
	); err != nil {
		return err
	}
	if err := applyEcoRuntime(rt); err != nil {
		return err
	}
	rt.activeConversationID = record.ID
	return nil
}

func (rt *runtime) archiveAndClear() error {
	if rt == nil {
		return nil
	}
	if err := rt.saveConversation(); err != nil {
		return err
	}
	rt.activeConversationID = ""
	if rt.loop != nil {
		rt.loop.Reset()
	}
	return nil
}

func (rt *runtime) refreshREPLSession(session *ui.REPLSettings) {
	if rt == nil || session == nil {
		return
	}
	session.Model = rt.config.Model
	session.Effort = rt.config.Effort
	session.Context = rt.config.Context
	session.Fast = rt.config.Fast
}
