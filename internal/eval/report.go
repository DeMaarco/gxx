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

package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Summary struct {
	ID                                           string
	Passed, Failed, Review, Interrupted, Skipped int
	Tokens                                       int64
	Requests                                     int
	MedianMS, P95MS                              int64
	Cost                                         float64
	CostKnown                                    bool
}

func Summaries(report Report) []Summary {
	groups := map[string]*Summary{}
	times := map[string][]int64{}
	var order []string
	for _, attempt := range report.Attempts {
		id := attempt.Configuration.ID
		group := groups[id]
		if group == nil {
			group = &Summary{ID: id, CostKnown: true}
			groups[id] = group
			order = append(order, id)
		}
		switch attempt.Status {
		case "passed":
			group.Passed++
		case "failed":
			group.Failed++
		case "review_required":
			group.Review++
		case "interrupted":
			group.Interrupted++
		case "skipped":
			group.Skipped++
		}
		if attempt.Status != "skipped" {
			times[id] = append(times[id], attempt.DurationMS)
		}
		group.Tokens += attempt.Usage.TotalTokens
		group.Requests += attempt.Requests
		if attempt.CostUSD == nil {
			if attempt.Status != "skipped" {
				group.CostKnown = false
			}
		} else {
			group.Cost += *attempt.CostUSD
		}
	}
	var summaries []Summary
	for _, id := range order {
		g := groups[id]
		values := times[id]
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		if len(values) > 0 {
			g.MedianMS = values[(len(values)-1)/2]
			g.P95MS = values[int(math.Ceil(.95*float64(len(values))))-1]
		}
		summaries = append(summaries, *g)
	}
	return summaries
}

func Markdown(report Report, baseline *Report) (string, error) {
	if baseline != nil && (baseline.Version != report.Version || baseline.SuiteHash != report.SuiteHash || baseline.Mode != report.Mode) {
		return "", fmt.Errorf("baseline requires the same report version, suite hash and execution mode")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# gxx evaluation\n\nMode: **%s** · Revision: `%s` · Suite: `%s`\n\n", report.Mode, md(report.Revision), report.SuiteHash)
	fmt.Fprintf(&b, "Limits: %d trials per case/configuration, %d steps per turn, %d ms per case.\n\n", report.Trials, report.MaxSteps, report.CaseTimeoutMS)
	if report.Mode != "live" {
		b.WriteString("**Offline simulation validates orchestration, tools and graders. It does not measure model quality, real token savings or provider latency.**\n\n")
	}
	b.WriteString("| Configuration | Passed | Failed | Manual review | Interrupted | Not run | Success / attempted | Tokens | Requests | Estimated USD | Median ms | P95 ms |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, s := range Summaries(report) {
		cost := "unknown"
		if report.Mode == "live" && s.CostKnown {
			cost = fmt.Sprintf("%.6f", s.Cost)
		}
		n := s.Passed + s.Failed + s.Review + s.Interrupted
		rate := "n/a"
		if n > 0 {
			rate = fmt.Sprintf("%.1f%%", 100*float64(s.Passed)/float64(n))
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %s | %d | %d | %s | %d | %d |\n", md(s.ID), s.Passed, s.Failed, s.Review, s.Interrupted, s.Skipped, rate, s.Tokens, s.Requests, cost, s.MedianMS, s.P95MS)
	}
	b.WriteString("\nCost uses locally available rates and reported usage; it is an estimate, not a subscription bill. Manual reviews are not counted as passes. Token limits are checked between requests and may be exceeded by the final in-flight response. Failed requests may have unreported usage.\n")
	if baseline != nil {
		fmt.Fprintf(&b, "\n## Baseline\n\nRevision: `%s`. Counts are shown separately; compare identical configurations and completed trials before drawing conclusions.\n\n", md(baseline.Revision))
		fmt.Fprintf(&b, "Baseline limits: %d trials, %d steps per turn, %d ms per case.\n\n", baseline.Trials, baseline.MaxSteps, baseline.CaseTimeoutMS)
		b.WriteString("| Configuration | Passed | Failed | Manual review | Interrupted | Not run | Tokens |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, s := range Summaries(*baseline) {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d |\n", md(s.ID), s.Passed, s.Failed, s.Review, s.Interrupted, s.Skipped, s.Tokens)
		}
	}
	b.WriteString("\n## Attempts\n\n| Case | Configuration | Trial | Status | Reason |\n| --- | --- | ---: | --- | --- |\n")
	for _, a := range report.Attempts {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s |\n", md(a.Case), md(a.Configuration.ID), a.Trial, a.Status, md(a.Reason))
	}
	for _, a := range report.Attempts {
		if len(a.Rubric) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### Manual review: %s / %s / %d\n\n", md(a.Case), md(a.Configuration.ID), a.Trial)
		for _, item := range a.Rubric {
			fmt.Fprintf(&b, "- %s\n", md(item))
		}
	}
	return b.String(), nil
}

func md(value string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "`", "'").Replace(value)
}
