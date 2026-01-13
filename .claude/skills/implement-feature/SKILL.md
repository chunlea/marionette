---
name: implement-feature
description: |
  Autonomous feature implementation with minimal supervision.
  Use this skill when:
  - User wants to implement a new feature ("implement", "build", "create")
  - User wants autonomous development mode ("auto-dev")
  - User wants to update or check progress ("update roadmap", "check progress")
  - User wants to continue previous work ("continue", "resume")
triggers:
  - implement feature
  - build feature
  - create feature
  - add feature
  - autonomous development
  - auto-dev
  - update roadmap
  - check progress
  - continue task
  - resume work
tools:
  - Read
  - Write
  - Edit
  - Bash
  - Glob
  - Grep
  - Task
  - TodoWrite
---

# Implement Feature Skill

Autonomous feature implementation with self-review loop and progress tracking.

## Capabilities

### 1. Autonomous Development
Triggered by: "implement streaming manager", "auto-dev", "build user auth"

Execute feature development with minimal human intervention.

### 2. Progress Tracking
Triggered by: "update roadmap", "check progress", "what's the status"

Track and update development progress in `.claude/current-task.md`.

### 3. Resume Work
Triggered by: "continue", "resume", "continue previous task"

Resume interrupted work from saved state.

## Execution Flow

```
┌───────────────────────────────────────────────────────────┐
│                     Feature Request                       │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────┐
│  Phase 1: Analysis (Autonomous)                           │
│  - Analyze feature request                                │
│  - Explore existing codebase                              │
│  - Determine implementation approach                      │
│  - Only ask user for major architectural decisions        │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────┐
│  Phase 2: Branch Preparation (Autonomous)                 │
│  - Create feature branch: feature/<feature-name>          │
│  - Base on latest main                                    │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────┐
│  Phase 3: Implementation Loop (Autonomous)                │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Implement → Commit → Self-review → Fix → Continue   │  │
│  └─────────────────────────────────────────────────────┘  │
│  Update .claude/current-task.md after each major step     │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────┐
│  Phase 4: Verification (Autonomous)                       │
│  - Run tests: make test                                   │
│  - Run linter: make lint                                  │
│  - Verify build: make build                               │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────┐
│  Phase 5: PR Creation (Autonomous)                        │
│  - gh pr create with summary                              │
│  - Notify user for final review                           │
└───────────────────────────────────────────────────────────┘
```

## Decision Authority

| Decision Type            | Authority                            |
|--------------------------|--------------------------------------|
| File structure changes   | ✅ Autonomous                        |
| Function/method design   | ✅ Autonomous                        |
| Interface design         | ✅ Autonomous (follow patterns)      |
| Adding new dependencies  | ⚠️ Ask user                          |
| Architecture changes     | ⚠️ Ask user                          |
| Removing existing features | ⚠️ Ask user                        |

## Commit Convention

Atomic commits with conventional format:
```
feat: add streaming provider interface
feat: implement memory store
test: add provider unit tests
fix: handle nil pointer in Start()
docs: add provider usage example
```

## Self-Review Checklist

After each implementation unit:
- [ ] Logic correct
- [ ] Edge cases handled
- [ ] Errors handled properly
- [ ] Tests added
- [ ] No TODO/FIXME left unaddressed
- [ ] Code style consistent

## Completion Criteria

- [ ] All tests pass
- [ ] Self-review: no high-priority issues
- [ ] Code coverage > 80%
- [ ] Lint passes
- [ ] PR created

## State Tracking

Progress tracked in `.claude/current-task.md`:

```markdown
# Current Task

## Description
Brief description of the current feature/task

## Status
🔄 In Progress | ✅ Complete | ⏸️ Blocked

## Progress
- [x] Completed item 1
- [x] Completed item 2
- [ ] Current item  ← working on
- [ ] Pending item

## Branch
feature/branch-name

## Key Files
- path/to/main/file.go
- path/to/test/file_test.go

## Blockers
- [ ] Blocker description (if any)

## Next Session
Continue from: specific location or task

## History
- 2024-01-12: Started implementation
- 2024-01-12: Completed interface design
```

## Progress Update Actions

```
# View current status
check progress
update roadmap

# Mark item complete
update roadmap completed streaming provider interface

# Record blocker
update roadmap blocked waiting for API design decision

# Set next step
update roadmap next implement store layer
```

## Usage Examples

```
# Start autonomous development
implement streaming manager feature

# Implement with specific requirements
implement user authentication with JWT tokens

# Check current progress
check progress
what's the status

# Continue interrupted work
continue
resume previous task

# Update progress
update roadmap completed interface design
update roadmap blocked need API decision
```

## Integration with Other Skills

- Uses **dev-review** for self-review after commits
- Uses **parallel-dev** for worktree management when needed
- State automatically saved via SessionEnd hook
