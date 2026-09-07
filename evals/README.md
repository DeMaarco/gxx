# Development evaluations

`gxx-eval` is a repository development tool, not a new `gxx` subcommand. Each trial uses a fresh temporary workspace, the shared production session, the production tool registry and the selected provider. Personal skills are excluded. The agent sees fixture files and user turns, never the checks or offline script.

## Offline checks

```sh
go run ./cmd/gxx-eval --trials 1 --out eval-results-smoke
go run ./cmd/gxx-eval --matrix evals/matrix-eco.json --out eval-results-eco
go test -race ./test/...
go vet ./...
```

The 24 cases cover investigation, edits, web validation, Eco, compacted conversations, and permissions. Offline scripts exercise real tools and file graders without requesting model responses. They do **not** demonstrate better model quality, token savings, or provider latency. Provider integration tests use local mock HTTP servers to verify actual payloads, compaction, retry accounting and budgets. CI runs these offline tests on Linux, macOS and Windows.

The browser case requires existing `agent-browser` and `node` executables. Its check uses a private session named after the temporary workspace, verifies the Contact link, and closes the browser in the same command. Missing prerequisites are recorded as `skipped`; the test suite explicitly covers this case without installing a browser. A browser command failure is a failure, not a skip.

## Manual model comparisons

Use existing gxx credentials. Keep API keys and tokens out of suite and matrix files.

```sh
go run ./cmd/gxx-eval --live --matrix evals/matrix-models.json --max-tokens 300000 --max-requests 100 --out eval-results-baseline
go run ./cmd/gxx-eval --live --matrix evals/matrix-models.json --max-tokens 300000 --max-requests 100 --baseline eval-results-baseline/report.json --out eval-results-candidate
```

These are manual examples with explicit budgets, not a scheduled or automatic paid check. Adjust the matrix to models available to your accounts. `matrix.json` uses the current OpenAI default; `matrix-eco.json` varies only Eco; `matrix-models.json` compares explicit model/effort settings. Context is normalized by the production configuration and the effective value is recorded. No run changes your saved settings or selects a new default model.

Each configuration normally runs three trials per case, sequentially. Flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--suite` | `evals/suite.json` | Versioned case definitions |
| `--matrix` | `evals/matrix.json` | Explicit provider/model/effort/context/Eco combinations |
| `--trials` | `3` | Repetitions per case/configuration |
| `--max-steps` | `24` | Production model-step limit per user turn |
| `--case-timeout` | `2m` | Whole-case deadline, including grading |
| `--live` | `false` | Enable provider requests |
| `--max-tokens` | `0` | Required positive total reported-token budget for live runs |
| `--max-requests` | `0` | Required positive provider-attempt budget for live runs |
| `--trace` | `false` | Local tool-event metadata, without raw arguments, outputs or reasoning |
| `--out` | `eval-results` | New directory for reports; existing directories are refused |
| `--baseline` | empty | Earlier JSON report with matching version, suite hash and execution mode |

Retries and model-written summaries consume the same global budget as normal responses. No further model request starts once either limit is reached. Tokens are known after a response, so the final in-flight response can exceed the remaining token budget. Failed requests can have unreported usage; reports count those requests separately. The dollar figure uses locally available pricing and reported usage; it is an estimate, not an enforced dollar cap or an OAuth subscription bill. Unknown cost is never reported as zero.

## Reading results

Reports contain the source revision, suite hash, effective configuration, per-attempt checks, answers, tokens, requests, compact counts, time, and estimated cost where available. Keep the source revision and matrix alongside any comparison; `+dirty` means the source included uncommitted work.

- `passed`: all automated checks passed.
- `failed`: an agent error or at least one failed check.
- `review_required`: automated checks passed in a live run, but the case's human rubric remains pending. It is not counted as a success.
- `interrupted`: a started trial hit its budget, timeout, or cancellation.
- `skipped`: the trial never ran because of missing prerequisites, credentials, cancellation, or exhausted budget.

`report.md` compares quality, tokens, requests and median/P95 duration separately. A baseline table is included when requested. Compare identical configurations and completed trials; small samples and unfinished manual reviews do not establish a quality improvement. Exit code 1 means a failed/interrupted run, missing credentials/budget, or no attempts ran; dependency skips are allowed alongside completed cases. Code 2 is invalid input; 130 is cancellation. Reports are still saved after interrupted evaluations.

Reports stay local under ignored `eval-results*` directories. Known loaded credentials are redacted, and request instrumentation never records credential headers or raw provider errors. Answers may contain fixture content, so use synthetic fixtures and review artifacts before sharing. Credentials refreshed during a live session are not part of a trace. Nothing is uploaded automatically.

## Adding a regression

Copy `case-template.json`, replace its scenario and assertions, then append its case to `suite.json`. Each case declares:

- `id`, `category`, `mode` (`ask`, `plan`, `agent`), and `permission` (`ask`, `auto-writes`, `auto`).
- `fixture`: workspace-relative file paths mapped to initial contents. Paths cannot escape the workspace or write `.git` internals.
- Optional `git: true` creates a local initial commit. `initial_changes` then simulates user edits and untracked files; no global Git configuration is changed.
- `turns`: prompts, with optional `compact_after: true` to exercise explicit compaction.
- `checks`: assertions evaluated outside the model. Kinds are `file_contains`, `file_equals`, `file_unchanged`, `file_absent`, `answer_contains`, `answer_not_contains`, `final_answer_contains`, `tool_called`, `tool_failed`, `tool_succeeded`, and `command`. Tool checks support `min`; command checks use the production command runner and require a successful exit. Add `file_unchanged` checks for fixture tests to prevent a model from weakening its grader.
- Optional `requires`: executable names. Optional `rubric`: explicit human criteria for explanation or visual quality.
- `script`: offline-only `ModelResponse` values (`Text`, `ToolCalls`, `Usage`). Tool calls use the same `id`, `name`, `arguments` format as production. Supply a response without tool calls to finish each user turn. Never use invented token usage to claim real savings.

A useful regression reproduces a real failure with the smallest fixture that captures it. Check outcomes, not a prescribed sequence of tools, unless that sequence is the behavior under test. For continuity, assert against the final answer rather than allowing earlier answers to satisfy the check. Failing checks cannot be overridden by the offline script or the model's self-report.
