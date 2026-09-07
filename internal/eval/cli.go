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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gxx/internal/config"
)

func CLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gxx-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	suitePath := flags.String("suite", "evals/suite.json", "versioned evaluation suite")
	matrixPath := flags.String("matrix", "evals/matrix.json", "explicit model/effort/context/eco configurations")
	out := flags.String("out", "eval-results", "new local report directory (must not exist)")
	baselinePath := flags.String("baseline", "", "optional report.json from an identical suite and execution mode")
	options := Options{}
	flags.BoolVar(&options.Live, "live", false, "make real provider requests (requires both budgets)")
	flags.IntVar(&options.Trials, "trials", 3, "repetitions per case and configuration")
	flags.Int64Var(&options.MaxTokens, "max-tokens", 0, "total reported token budget; checked between requests")
	flags.IntVar(&options.MaxRequests, "max-requests", 0, "maximum provider attempts including retries and summaries")
	flags.DurationVar(&options.CaseTimeout, "case-timeout", 2*time.Minute, "maximum duration per case")
	flags.IntVar(&options.MaxSteps, "max-steps", config.DefaultMaxSteps, "maximum model steps per turn")
	flags.BoolVar(&options.Trace, "trace", false, "record local tool event metadata (no raw provider bodies)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || options.Trials < 1 || options.CaseTimeout <= 0 || options.MaxSteps < 1 {
		fmt.Fprintln(stderr, "invalid arguments or execution limits")
		return 2
	}
	var suite Suite
	var matrix Matrix
	if err := readJSON(*suitePath, &suite); err != nil {
		fmt.Fprintln(stderr, "read suite:", err)
		return 2
	}
	if err := readJSON(*matrixPath, &matrix); err != nil {
		fmt.Fprintln(stderr, "read matrix:", err)
		return 2
	}
	if err := Validate(suite, matrix, options); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var baseline *Report
	if *baselinePath != "" {
		baseline = &Report{}
		if err := readJSON(*baselinePath, baseline); err != nil {
			fmt.Fprintln(stderr, "read baseline:", err)
			return 2
		}
		encoded, _ := json.Marshal(suite)
		hash := sha256.Sum256(encoded)
		if baseline.Version != Version || baseline.SuiteHash != hex.EncodeToString(hash[:]) || baseline.Mode != modeName(options.Live) {
			fmt.Fprintln(stderr, "baseline requires the same report version, suite hash and execution mode")
			return 2
		}
	}
	if _, err := os.Stat(*out); !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "report directory must not exist")
		return 2
	}
	if data, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output(); err == nil {
		options.Revision = strings.TrimSpace(string(data))
		if dirty, err := exec.CommandContext(ctx, "git", "status", "--porcelain").Output(); err == nil && len(dirty) > 0 {
			options.Revision += "+dirty"
		}
	} else {
		options.Revision = "unknown"
	}
	if options.Live {
		cwd, _ := os.Getwd()
		options.Credentials = config.Load(cwd)
	}
	fmt.Fprintf(stderr, "Running %s: %d cases × %d configurations × %d trials\n", modeName(options.Live), len(suite.Cases), len(matrix.Configurations), options.Trials)
	report, err := Run(ctx, suite, matrix, options)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	markdown, err := Markdown(report, baseline)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "encode report:", err)
		return 1
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		fmt.Fprintln(stderr, "create report directory:", err)
		return 1
	}
	for name, data := range map[string][]byte{"report.json": append(encoded, '\n'), "report.md": []byte(markdown)} {
		if err := os.WriteFile(filepath.Join(*out, name), data, 0600); err != nil {
			fmt.Fprintln(stderr, "write report:", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "Reports: %s\n", *out)
	if ctx.Err() != nil {
		return 130
	}
	attempted := false
	for _, attempt := range report.Attempts {
		attempted = attempted || attempt.Status != "skipped"
		if attempt.Status == "failed" || attempt.Status == "interrupted" {
			return 1
		}
		if attempt.Status == "skipped" && !strings.HasPrefix(attempt.Reason, "dependency_unavailable:") {
			return 1
		}
	}
	if !attempted {
		return 1
	}
	return 0
}

func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one JSON document")
	}
	return nil
}
