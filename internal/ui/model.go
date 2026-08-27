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

package ui

import (
	"fmt"
	"strings"

	"gxx/internal/config"
)

var bundledModels = []string{
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
}

var effortValues = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

type pickerMode int

const (
	pickerClosed pickerMode = iota
	pickerModels
	pickerOptions
	pickerModes
	pickerContext
)

const (
	optionContext = 0
	optionEffort  = 1
	optionFast    = 2
	optionCount   = 3
)

type modelCommand struct {
	Show    bool
	Model   string
	Context string
	Effort  string
	Fast    *bool
}

func catalogModels(current string) []string {
	current = config.CanonicalModel(current)
	seen := map[string]struct{}{}
	models := make([]string, 0, len(bundledModels)+1)
	if current != "" {
		models = append(models, current)
		seen[current] = struct{}{}
	}
	for _, model := range bundledModels {
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func cycleValue(values []string, current string, delta int) string {
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	index = (index + delta) % len(values)
	if index < 0 {
		index += len(values)
	}
	return values[index]
}

func parseFastFlag(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("fast must be on or off")
	}
}

func parseModelCommand(line string) (modelCommand, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/model" {
		return modelCommand{}, fmt.Errorf("not a model command")
	}
	if len(fields) == 1 {
		return modelCommand{Show: true}, nil
	}

	var command modelCommand
	for index := 1; index < len(fields); index++ {
		field := fields[index]
		key, value, ok := strings.Cut(field, "=")
		if ok {
			if err := assignModelField(&command, key, value); err != nil {
				return modelCommand{}, err
			}
			continue
		}
		switch field {
		case "context", "effort", "fast":
			if index+1 >= len(fields) {
				return modelCommand{}, fmt.Errorf("%s requires a value", field)
			}
			index++
			if err := assignModelField(&command, field, fields[index]); err != nil {
				return modelCommand{}, err
			}
		default:
			if command.Model != "" {
				return modelCommand{}, fmt.Errorf("unexpected model argument %q", field)
			}
			command.Model = config.CanonicalModel(field)
		}
	}
	return command, nil
}

func assignModelField(command *modelCommand, key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "model":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("model cannot be empty")
		}
		command.Model = config.CanonicalModel(value)
	case "context":
		normalized, err := config.NormalizeContext(value)
		if err != nil {
			return err
		}
		command.Context = normalized
	case "effort":
		if err := config.ValidateEffort(value); err != nil {
			return err
		}
		command.Effort = strings.TrimSpace(value)
	case "fast":
		fast, err := parseFastFlag(value)
		if err != nil {
			return err
		}
		command.Fast = &fast
	default:
		return fmt.Errorf("unknown model setting %q", key)
	}
	return nil
}

func formatModelStatus(model, context, effort string, fast bool) string {
	fastLabel := "off"
	if fast {
		fastLabel = "on"
	}
	return fmt.Sprintf(
		"model %s · context %s · effort %s · fast %s",
		model,
		context,
		effort,
		fastLabel,
	)
}

func encodeModelCommand(model, context, effort string, fast bool) string {
	fastLabel := "off"
	if fast {
		fastLabel = "on"
	}
	return fmt.Sprintf(
		"/model %s context=%s effort=%s fast=%s",
		model,
		context,
		effort,
		fastLabel,
	)
}
