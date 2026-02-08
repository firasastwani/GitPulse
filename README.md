# GitPulse

**AI-powered auto-commit tool** — watches your files, groups changes semantically, generates conventional commit messages, runs AI code review, and commits & pushes on demand or via a safety timer.

---

## Features

- **File watching** — Debounced fsnotify watcher tracks changes in real time
- **Semantic grouping** — Heuristic clustering (directory, name affinity, file type) + Claude refinement
- **AI commit messages** — Conventional commits format, specific and descriptive
- **AI code review** — Pre-push review for bugs, logic errors, security issues; blocks push if blockers found
- **Interactive review flow** — Fix manually, let AI fix, or continue anyway
- **History & dashboard** — JSON store with diffs, line stats, review data; web dashboard to visualize work
- **Multi-project** — Run `gitpulse init` in any repo, use `-C path` or `gitpulse push -C path` from anywhere

---

## Quick Start

### Prerequisites

- Go
- [Anthropic API key]— set `ANTHROPIC_API_KEY` or `CLAUDE_API_KEY` in `.env`

### Build

```bash
go build -o gitpulse .
```

### One-time setup in any repo

```bash
cd /path/to/your/project
gitpulse init
```

Creates `.gitpulse/config.yaml` and adds `.gitpulse/` and `.gitpulse.pid` to `.gitignore`.

### Run

```bash
cd /path/to/your/project
gitpulse
```

Or from anywhere:

```bash
gitpulse -C /path/to/your/project
```

### Trigger commit & push

With the daemon running:

- **Same terminal:** Press ENTER
- **Other terminal:** `gitpulse push -C /path/to/your/project`

### Dashboard

```bash
gitpulse dashboard -C /path/to/your/project --port 8080
```

Opens the Effects Dashboard at `http://localhost:8080` — commit history, diffs, line stats, review findings.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              main.go                                     │
│  Commands: init | push | dashboard | daemon (default)                    │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           Engine (orchestrator)                          │
│  • Buffers changes from Watcher                                          │
│  • Triggers on ENTER, gitpulse push, or safety timer                     │
│  • Runs pipeline: group → AI refine → AI review → stage → commit → push  │
└─────────────────────────────────────────────────────────────────────────┘
         │              │              │              │              │
         ▼              ▼              ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│   Watcher   │ │   Grouper   │ │     AI      │ │    Git      │ │    Store    │
│  fsnotify   │ │  heuristic  │ │   Claude    │ │  go-git +   │ │  JSON file  │
│  debounced  │ │  clusters   │ │  refine +   │ │  shell git  │ │  history    │
│  change set │ │  files      │ │  review +   │ │  stage,     │ │  .gitpulse/ │
│             │ │             │ │  fix        │ │  commit,    │ │  history.   │
│             │ │             │ │             │ │  push       │ │  json       │
└─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘
```

### Pipeline flow

1. **Watcher** — Emits `ChangeSet` (batch of file paths) after debounce delay
2. **Grouper** — Pre-groups by directory, name affinity (e.g. `foo.go` + `foo_test.go`), singletons
3. **Git** — Fetches real unified diffs per file (`git diff HEAD -- file`)
4. **AI Refine** — Claude refines groupings and generates specific conventional commit messages
5. **AI Review** — Claude reviews diffs for bugs, security issues, logic errors
6. **Interactive gate** — If blockers: user chooses [1] Fix manually, [2] Let AI fix, [3] Continue anyway
7. **Stage & commit** — Per group: `git add`, `git commit` with AI message
8. **Store** — Saves enriched `CommitRecord` (files, diffs, line stats, review findings) to `.gitpulse/history.json`
9. **Push** — `git push` if `auto_push: true`, then `MarkPushed` updates store

### Package overview

| Package              | Responsibility                                                                                       |
| -------------------- | ---------------------------------------------------------------------------------------------------- |
| `internal/watcher`   | fsnotify + debounce; emits `ChangeSet`                                                               |
| `internal/grouper`   | Heuristic grouping: directory, name affinity, singletons                                             |
| `internal/git`       | `GetFileDiff`, `StageFiles`, `Commit`, `Push`, `ResetStaging`                                        |
| `internal/ai`        | Claude API: `RefineAndCommit`, `ReviewCode`, `GenerateFix` (patch-based)                             |
| `internal/store`     | JSON append store: `Save`, `Recent`, `GetByHash`, `GetByFile`, `Stats`, `MarkPushed`                 |
| `internal/ui`        | Logger, `ReviewFindings`, `PromptReviewAction`, `WaitForManualFix`                                   |
| `internal/config`    | YAML + `.env`; `LoadFromDir`, `WriteDefault`                                                         |
| `internal/dashboard` | HTTP server + embedded static UI; serves `/api/stats`, `/api/history`, `/api/commits/`, `/api/files` |

---

## Configuration

Config is loaded from `config.yaml` or `.gitpulse/config.yaml` in the project directory. `gitpulse init` writes a default.

```yaml
watch_path: "."
debounce_seconds: 900 # safety timer (auto-flush if you forget to push)
auto_push: true
remote: "origin"
branch: "main"

ai:
  provider: "claude"
  model: "claude-sonnet-4-5"
  code_review: true # enable pre-push AI review

ignore_patterns:
  - "*.log"
  - "node_modules/"
  - ".git/"
  - ".gitpulse/"
```

**Environment:** `ANTHROPIC_API_KEY` or `CLAUDE_API_KEY` (from `.env` or shell).

---

## Data & History

- **Location:** `<project>/.gitpulse/history.json`
- **Format:** Array of `CommitRecord` — hash, message, files (with diffs, line stats), group reason, review findings, push metadata
- **Dashboard API:**
  - `GET /api/stats` — totals (commits, files, lines, reviews)
  - `GET /api/history` — all commits (newest first)
  - `GET /api/commits/<hash>` — single commit with full diff
  - `GET /api/files?path=...` — commits touching a file

---

## Safety & behavior

- **Safety timer** — If you don’t press ENTER or run `gitpulse push`, the timer auto-flushes after `debounce_seconds` (non-interactive, so no review prompt)
- **Non-interactive mode** — When triggered by timer or `SIGUSR1` without a TTY, review runs but does not block; findings are logged
- **Patch-based AI fix** — AI returns `old_code` / `new_code` JSON; only that snippet is replaced to avoid truncating large files
- **Max review iterations** — 3 re-review loops to prevent infinite loops

---

## UGAHacks Project Log

### Project Information

- **Project Title:** GitPulse
- **Team Members:** Firas Astwani
- **Tier Level:** Advanced
- **Project Description:** A daemon running in your local dev folder, automatically manages git commands with semantic intelligence, runs quality checks, and makes autonomous push/PR decisions using AI. Learning GoLang.

---

### Friday

**Goals:**

- Plan/Scaffold file structure + data layer
- Initialize file tracking and diff generation
- Possibly look into beginning semantic file diffs
- Implement Git management

**Progress:**

- Finished a lot more than expected today. Implemented the full file change tracking + git connection
- Also implemented AI-generated "commit message"; did not get to the code review yet, task for tomorrow
- Created the main "engine" that orchestrates the different pieces to trigger one another
- Toyed with the terminal to find best UX for user convenience
- Successful day 1, ready for day 2

**Challenges:**

1. **Need the user's macOS keychain credentials to actually use git push**

   - Solution: For now, using `exec.Command` in Go to shell out to git push (uses system credential helper)

2. **Too difficult to track file diffs by hand** — thought to cache a state of the files every x minutes then check diff on file changes, but the implementation would take too long

   - Solution: Using `git diff` instead through the go-git package

3. **UX design issue** — 5 min debounce timer could spam push half-baked code
   - Solution: Switched to a 15 min debounce timer + manual terminal push design choice that gives the user more control over when a push occurs

**Learning:**

- Learned how to use the fsnotify Go library to track file changes

**AI Usage:** Cursor — Draft test files, plan early stages of project. Helped check edge cases and fix them.

---

### Saturday

**Goals:**

- Create the AI review component; separate reviews by semantic file groupings
- Make commit messages more specific to actual file changes (done with better prompting/example prompting)
- Add a loading indicator while AI fixes
- Enhanced JSON file storage with the hopes of adding a simple WebUI

**Progress:**

- Lots of progress today. Created the AI review + fix components using Anthropic API, plus checking semantic groupings and making sure the correct files are combined
- Was able to make commit messages significantly better with improved prompting
- Wasn't able to make a loading indicator — didn't have time
- Made a very simple UI to show off the changes made using GitPulse
- Made it easily usable in any repo the user wants — 2 simple commands

**Challenges:**

1. **Defining errors identified during the check across different files in a struct format**

   - Solution: Include connected errors from different files in the initial ErrorStruct (related locations)

2. **Code review implementation not catching obvious errors in code syntax**

   - Solution: Fixed — wasn't catching errors because the test was still passing a toy file diff used for testing

3. **Code review hangs upon selecting an option**

   - Solution: Two competing stdin readers in main caused a race leaving the terminal hanging. Switched to a single stdin channel shared between the main loop and interactive prompts

4. **The AI fix selection would delete half the file's code away**
   - Solution: Switched to a patch-based method that feeds only the required section of the broken file rather than the whole file, because it was hitting max tokens

**Learning:**

- Best practices for designing this kind of project
- Go syntax and idioms over the course of the project

**AI Usage:** Cursor — Helped a lot with planning and system design, syntax for specific libraries, and debugging tedious logic errors.
