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

package pricing

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

var datedSuffix = regexp.MustCompile(`-\d{8}$`)

func parseOpenAI(body []byte) map[rateKey]Rate {
	rates := map[rateKey]Rate{}
	fast := false
	skip := false
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "standard pricing data"):
			fast, skip = false, false
		case strings.Contains(lower, "fast pricing data"):
			fast, skip = true, false
		case strings.Contains(lower, "batch pricing data"),
			strings.Contains(lower, "flex pricing data"):
			skip = true
		}
		if skip || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := tableCells(line)
		if len(cells) < 5 || isTableSep(cells) || strings.EqualFold(cells[0], "model") {
			continue
		}
		id := openaiModelID(cells[0])
		if id == "" {
			continue
		}
		short := rateFromCells(cells, 1)
		if short.Input > 0 || short.Output > 0 {
			putRate(rates, rateKey{model: id, fast: fast}, short)
		}
		if len(cells) >= 9 {
			long := rateFromCells(cells, 5)
			if long.Input > 0 || long.Output > 0 {
				putRate(rates, rateKey{model: id, fast: fast, long: true}, long)
			}
		}
	}
	return rates
}

func parseAnthropic(body []byte) map[rateKey]Rate {
	rates := map[rateKey]Rate{}
	section := "standard"
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "fast mode pricing"):
			section = "fast"
		case strings.HasPrefix(lower, "## model pricing"):
			section = "standard"
		case strings.HasPrefix(lower, "## "):
			section = "other"
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := tableCells(line)
		if len(cells) < 3 || isTableSep(cells) {
			continue
		}
		head := strings.ToLower(cells[0])
		if head == "model" || head == "cache operation" || head == "concept" {
			continue
		}
		switch section {
		case "standard":
			if len(cells) < 6 {
				continue
			}
			for _, id := range claudeModelIDs(cells[0]) {
				putRate(rates, rateKey{model: id}, Rate{
					Input:      parseMoney(cells[1]),
					CacheWrite: parseMoney(cells[2]),
					Cached:     parseMoney(cells[4]),
					Output:     parseMoney(cells[5]),
				})
			}
		case "fast":
			for _, id := range claudeModelIDs(cells[0]) {
				putRate(rates, rateKey{model: id, fast: true}, Rate{
					Input:  parseMoney(cells[1]),
					Output: parseMoney(cells[2]),
				})
			}
		}
	}
	return rates
}

func putRate(rates map[rateKey]Rate, key rateKey, rate Rate) {
	if rate.Input <= 0 && rate.Output <= 0 {
		return
	}
	if _, exists := rates[key]; exists {
		return
	}
	rates[key] = rate
}

func rateFromCells(cells []string, start int) Rate {
	if start+3 >= len(cells) {
		return Rate{}
	}
	return Rate{
		Input:      parseMoney(cells[start]),
		Cached:     parseMoney(cells[start+1]),
		CacheWrite: parseMoney(cells[start+2]),
		Output:     parseMoney(cells[start+3]),
	}
}

func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isTableSep(cells []string) bool {
	for _, cell := range cells {
		trimmed := strings.Trim(cell, " :-")
		if trimmed != "" {
			return false
		}
	}
	return len(cells) > 0
}

func openaiModelID(name string) string {
	name = stripNote(name)
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "o1") ||
		strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4") {
		return stripDatedSuffix(name)
	}
	return ""
}

func claudeModelIDs(name string) []string {
	var ids []string
	for _, part := range strings.Split(name, "/") {
		id := claudeModelID(part)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func claudeModelID(name string) string {
	name = stripNote(name)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.Join(strings.Fields(name), "-")
	if !strings.Contains(name, "claude") &&
		!strings.Contains(name, "fable") &&
		!strings.Contains(name, "mythos") &&
		!strings.Contains(name, "opus") &&
		!strings.Contains(name, "sonnet") &&
		!strings.Contains(name, "haiku") {
		return ""
	}
	if !strings.HasPrefix(name, "claude-") {
		name = strings.TrimPrefix(name, "claude")
		name = strings.TrimPrefix(name, "-")
		name = "claude-" + name
	}
	return name
}

func stripNote(name string) string {
	if index := strings.Index(name, "("); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

func stripDatedSuffix(id string) string {
	return datedSuffix.ReplaceAllString(id, "")
}

func parseMoney(cell string) float64 {
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "-" || strings.EqualFold(cell, "n/a") {
		return 0
	}
	cell = strings.ReplaceAll(cell, ",", "")
	cell = strings.TrimPrefix(cell, "$")
	fields := strings.Fields(cell)
	if len(fields) == 0 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return value
}
