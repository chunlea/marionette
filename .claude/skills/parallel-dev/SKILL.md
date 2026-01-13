---
name: parallel-dev
description: |
  Manage parallel development using git worktrees.
  Use this skill when:
  - User wants to work on multiple features simultaneously
  - User mentions worktrees ("create worktree", "switch worktree", "list worktrees")
  - User wants to parallelize development ("work in parallel")
  - User asks about worktree status
triggers:
  - create worktree
  - switch worktree
  - list worktrees
  - worktree status
  - parallel development
  - work in parallel
  - remove worktree
tools:
  - Bash
  - Read
  - Write
  - TodoWrite
---

# Parallel Development Skill

Manage multiple concurrent development streams using git worktrees.

## Capabilities

### 1. List Worktrees
Triggered by: "list worktrees", "show worktrees"

```bash
git worktree list
```

Output format:
```
/path/to/marionette                 abc1234 [main]
/path/to/marionette.worktrees/alice def5678 [alice/streaming]
/path/to/marionette.worktrees/bob   ghi9012 [bob/auth]
```

### 2. Create Worktree
Triggered by: "create worktree alice", "create worktree for streaming"

```bash
# Create worktree with new branch
git worktree add ../marionette.worktrees/<name> -b <name>/main

# Create worktree from existing branch
git worktree add ../marionette.worktrees/<name> <existing-branch>
```

### 3. Worktree Status
Triggered by: "worktree status", "current worktree"

Shows:
- Current worktree name
- Current branch
- Uncommitted changes
- Related PRs

### 4. Switch Helper
Triggered by: "switch to worktree bob", "go to alice"

Outputs the path and command:
```bash
cd ../marionette.worktrees/bob
# Then start new Claude session
claude
```

### 5. Remove Worktree
Triggered by: "remove worktree alice", "delete worktree"

```bash
git worktree remove ../marionette.worktrees/<name>
git worktree prune
```

## Worktree Naming Convention

Use short, memorable names:
- `alice`, `bob`, `carol`, `dave`, `eve`, `frank`, `grace`, `henry`, `iris`, `jack`

Or feature-based:
- `streaming`, `auth`, `tunnel`, `desktop`

## Directory Structure

```
marionette/
├── marionette/                    # Main repository
│   └── .git/
└── marionette.worktrees/          # Worktrees directory
    ├── alice/                     # Feature A
    ├── bob/                       # Feature B
    └── carol/                     # Feature C
```

## Parallel Development Pattern

```
┌───────────────────────────────────────────────────────────┐
│                     Main Repository                       │
│                          main                             │
└───────────────────────────────────────────────────────────┘
              │              │              │
              ▼              ▼              ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   Worktree A    │  │   Worktree B    │  │   Worktree C    │
│  alice/stream   │  │    bob/auth     │  │  carol/tunnel   │
│                 │  │                 │  │                 │
│ Claude Instance │  │ Claude Instance │  │ Claude Instance │
│       #1        │  │       #2        │  │       #3        │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

## Workflow

### Setup Parallel Development
```
1. create worktree alice for streaming
2. create worktree bob for auth
3. Open Terminal 1: cd marionette.worktrees/alice && claude
4. Open Terminal 2: cd marionette.worktrees/bob && claude
5. Each instance works independently
```

### Worktree Lifecycle
```
Create → Develop → PR → Merge → Remove
   │                              │
   └──────── Keep for next ───────┘
             feature
```

## Best Practices

1. **One feature per worktree**
   - Keep worktrees focused
   - Avoid mixing unrelated changes

2. **Regular sync with main**
   - Rebase frequently to avoid conflicts
   - `git fetch origin && git rebase origin/main`

3. **Clean up when done**
   - Remove worktrees after PR merge
   - Run `git worktree prune` periodically

4. **Independent Claude sessions**
   - Each worktree gets its own Claude instance
   - Sessions don't interfere with each other

## Usage Examples

```
# List all worktrees
list worktrees

# Create worktree for new feature
create worktree alice
create worktree alice for streaming feature

# Check current worktree status
worktree status

# Help switch to another worktree
switch to worktree bob

# Remove completed worktree
remove worktree alice
```

## Session Start Context

When starting in a worktree, context is shown via SessionStart hook:
```
Worktree: alice | Branch: alice/streaming | Task: implement streaming manager
```

## Commands Reference

```bash
# Create
git worktree add ../marionette.worktrees/<name> -b <name>/main

# List
git worktree list

# Remove
git worktree remove ../marionette.worktrees/<name>

# Prune stale
git worktree prune
```
