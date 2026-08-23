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
claude --output-format stream-json --verbose \
    --print '<prompt>' > <name>.jsonl 2> <name>.err

# resume takes the session id from the `system`/`init` line of basic.jsonl
claude --output-format stream-json --verbose \
    --resume 080a38b4-4ab2-434d-927e-2f3103a3f56e \
    --print '<prompt>' > resume.jsonl 2> resume.err
```

The exact invocation is not identical across the three files. The CLI echoes
back what it actually ran with on the `system`/`init` line, and that is what
the tests assert against:

| File | `init.cwd` | `init.permissionMode` | `init.model` | `init.session_id` |
|------|-----------|----------------------|--------------|-------------------|
| `basic.jsonl` | `/private/var/folders/.../T/golden` | `auto` | `claude-fable-5` | `080a38b4-4ab2-434d-927e-2f3103a3f56e` |
| `tooluse.jsonl` | `.../scratchpad/golden` | `auto` | `claude-sonnet-5` | `5232e175-c2cc-4cf1-a2d1-b50a1850607e` |
| `resume.jsonl` | `/Users/chunlea/workspace/lab/marionette` | `acceptEdits` | `claude-sonnet-5` | `080a38b4-4ab2-434d-927e-2f3103a3f56e` |

`resume.jsonl` resumes the session created by `basic.jsonl` from a **different
working directory** and succeeds, so `--resume` is keyed on session id alone on
2.1.241. Nothing in the executor may assume the resumed session shares a cwd
with the session that created it.

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
