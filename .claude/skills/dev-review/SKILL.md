---
name: dev-review
description: |
  Automated development review workflow for code and design auditing.
  Use this skill when:
  - User wants to list PRs ("list PRs", "show open PRs")
  - User wants to review PRs ("review PR", "review code", "check PR #123")
  - User wants to audit design documents ("audit design", "review design")
  - User mentions numbered issues from previous review ("fix 1.1", "address 2.3")
triggers:
  - list PRs
  - show PRs
  - review PR
  - review code
  - audit design
  - check PR
  - fix issues
  - fix 1.
  - fix 2.
  - address feedback
tools:
  - Read
  - Grep
  - Glob
  - Bash
  - Edit
  - Write
  - TodoWrite
---

# Dev Review Skill

Comprehensive code and design review workflow.

## Capabilities

### 1. List PRs
Triggered by: "list PRs", "show open PRs", "what PRs are open"

```bash
# List your PRs
gh pr list --author @me

# List all PRs
gh pr list

# List PRs needing review
gh pr list --search "review-requested:@me"
```

Output format:
```
PR #123 [OPEN] feature/streaming-manager
  Author: @chunlea | Created: 2 days ago
  CI: passing | Reviews: 1/2 approved
  Labels: enhancement

PR #120 [DRAFT] feature/auth-middleware
  Author: @chunlea | Created: 5 days ago
  CI: failing | Reviews: none
```

### 2. PR Review
Triggered by: "review PR", "review PR #123", "check this PR"

Workflow:
1. Fetch PR diff and metadata via `gh pr view <number> --json`
2. Analyze code changes
3. Generate numbered issue list by priority
4. Track issues for batch fixing

Review Checklist:
- [ ] Logic correctness
- [ ] Edge case handling
- [ ] Error handling
- [ ] Concurrency safety
- [ ] Test coverage
- [ ] Code style
- [ ] Security

### 3. Design Audit
Triggered by: "audit design", "review design doc"

Multi-round review process:

**Round 1: Initial Audit**
- Architecture analysis
- Consistency check
- Completeness review
- Feasibility assessment

**Round 2: Deep Review**
- Verify proposed fixes
- Check for secondary issues
- Cross-reference with existing code

**Round 3: Final Verification**
- All issues addressed
- Design is implementable
- Ready for implementation

Design Checklist:
- [ ] All interfaces defined
- [ ] Error handling strategy clear
- [ ] Concurrency model specified
- [ ] Testing strategy outlined
- [ ] Dependencies identified
- [ ] Performance considerations
- [ ] Security review

### 4. Issue Fixing
Triggered by: "fix 1.1; 2.3", "address issues 1.1, 1.2", "fix all high priority"

Workflow:
1. Parse issue references from argument
2. Locate issues from previous review context
3. Fix each issue
4. Verify fix doesn't break other code
5. Commit fixes with descriptive messages
6. Report summary

Commit format:
```
fix: address review issue 1.1 - proper error handling in Start()
fix: address review issues 2.1, 2.2 - add input validation
```

## Output Formats

### Review Output
```markdown
## Review: [PR #123 | design-doc.md]

### High Priority
1.1 [file:line] Issue description
    Suggestion: how to fix

1.2 [file:line] Issue description
    Suggestion: how to fix

### Medium Priority
2.1 [file:line] Issue description

### Low Priority
3.1 [file:line] Issue description

### Summary
- High: N issues
- Medium: N issues
- Low: N issues

### Next Steps
To fix issues: "fix 1.1; 2.1; 3.1"
```

### Fix Output
```markdown
## Fix Summary

### Applied
- [x] 1.1: Description of fix @ `pkg/xxx/file.go:123`
- [x] 2.3: Description of fix @ `pkg/yyy/file.go:456`

### Skipped (needs user input)
- [ ] 1.2: Reason - requires architectural decision

### Commits
- abc123: fix: address review issue 1.1
- def456: fix: address review issues 2.3
```

## Usage Examples

```
# List open PRs
list PRs
show my open PRs

# Review current branch's PR
review the PR

# Review specific PR
review PR #123

# Audit design document
audit design docs/streaming.md

# Fix specific issues
fix 1.1; 2.3; 3.1

# Fix all high priority
fix all high priority issues

# After fixing, verify
review the PR again
```

## Quick Actions Flow

```
list PRs
    |
    v
review PR #123
    |
    v
fix 1.1; 1.2; 2.1
    |
    v
review PR again (verify fixes)
    |
    v
gh pr merge #123
```
