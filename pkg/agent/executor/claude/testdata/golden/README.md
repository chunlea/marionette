# Claude Code CLI golden recordings

Real, unedited `stream-json` output recorded from the Claude Code CLI.
These files are the source of truth for the message model in `messages.go`
and for the table tests in `parser_test.go`. Do not hand-edit them; re-record
instead.

| File | Scenario |
|------|----------|
| `basic.jsonl` | Single-turn prompt, no tools. Ends with `result`/`subtype:success`. |
| `tooluse.jsonl` | Prompt that drives a `Bash` tool call, tool result echoed back as a `user` message. |
| `resume.jsonl` | `--resume <session-id>` of the session created by `basic.jsonl`, recorded from a **different** working directory. |

## Recording environment

- CLI version: **2.1.241 (Claude Code)**
- Recorded: 2026-08-23
- Model: `claude-fable-5`

## Recording commands

```bash
claude --output-format stream-json --verbose --permission-mode acceptEdits \
    --print 'say hi' > basic.jsonl 2> basic.err

claude --output-format stream-json --verbose --permission-mode acceptEdits \
    --print 'run: echo marionette-golden' > tooluse.jsonl 2> tooluse.err

# session id taken from the `system`/`init` line of basic.jsonl,
# deliberately run from a different cwd to prove resume is not cwd-bound
claude --output-format stream-json --verbose --permission-mode acceptEdits \
    --resume 080a38b4-4ab2-434d-927e-2f3103a3f56e \
    --print 'say hi again' > resume.jsonl 2> resume.err
```

## Facts these recordings pin down

- `result` lines carry `result` as a **string**, `total_cost_usd` as a **number**,
  and report outcome through `subtype` (`success` / `error_max_turns` /
  `error_during_execution`) plus `is_error` (bool). There is no `success`,
  `exit_code` or `error` field.
- `permission_denials` is present but empty in every recording; its element shape
  is unverified, so it is modelled as raw JSON.
- `rate_limit_event` messages and `system` messages with subtypes
  `hook_started`, `hook_response` and `thinking_tokens` arrive mid-stream and
  must not end the turn.
- Assistant `usage` is per-request and cumulative-ish across a turn; the
  authoritative per-run totals come from the final `result` line's `usage`.
