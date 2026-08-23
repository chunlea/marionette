# Claude Code CLI golden recordings

Real, unedited `stream-json` output recorded from the Claude Code CLI.
These files are the source of truth for the message model in `messages.go`
and for the table tests in `parser_test.go`. Do not hand-edit them; re-record
instead.

| File | Scenario |
|------|----------|
| `basic.jsonl` | Single-turn prompt, no tools. Ends with `result`/`subtype:success`. |
| `tooluse.jsonl` | Prompt that drives a `Bash` tool call, tool result echoed back as a `user` message. |
| `resume.jsonl` | `--resume` of the session created by `basic.jsonl`, recorded from a **different** working directory. |

## Recording environment

- CLI version: **2.1.241 (Claude Code)**
- Recorded: 2026-08-23
- Model: `claude-sonnet-5` (all three)
- Permission mode: `auto` (all three)

All three share the same model, permission mode and CLI version so the fixtures
are self-consistent. `TestInitMessage_RecordingsAreConsistent` enforces that and
will fail if a future re-record drifts.

## Recording commands

`basic` and `tooluse` were each recorded in their own empty temporary directory.
`resume` was recorded from the repository root, deliberately not the directory
`basic` was created in.

```bash
# basic.jsonl  (cwd: an empty temp dir)
claude --output-format stream-json --verbose --model sonnet --permission-mode auto \
    --print 'Reply with exactly: hi' < /dev/null > basic.jsonl 2> basic.err

# tooluse.jsonl  (cwd: a different empty temp dir)
claude --output-format stream-json --verbose --model sonnet --permission-mode auto \
    --print 'Run this bash command and then reply with exactly its output: echo marionette-golden' \
    < /dev/null > tooluse.jsonl 2> tooluse.err

# resume.jsonl  (cwd: the repository root)
# the session id comes from the `system`/`init` line of basic.jsonl
claude --output-format stream-json --verbose --model sonnet --permission-mode auto \
    --resume c184bf5b-db6e-4705-a45f-eeb5c28f965b \
    --print 'Reply with exactly: hi again' < /dev/null > resume.jsonl 2> resume.err
```

`< /dev/null` matters: without it the CLI waits on stdin and writes a
"no stdin data received in 3s" warning to stderr.

| File | `init.cwd` | `init.session_id` |
|------|-----------|-------------------|
| `basic.jsonl` | an empty temp dir | `c184bf5b-db6e-4705-a45f-eeb5c28f965b` |
| `tooluse.jsonl` | a different empty temp dir | `698a1b66-8639-40c4-90f6-c061da925803` |
| `resume.jsonl` | `/Users/chunlea/workspace/lab/marionette` | `c184bf5b-db6e-4705-a45f-eeb5c28f965b` |

`resume.jsonl` resumes the session created by `basic.jsonl` from a **different
working directory** and succeeds, so `--resume` is keyed on session id alone on
2.1.241. Nothing in the executor may assume the resumed session shares a cwd
with the session that created it.

Recording `resume` inside this repository also picks up its `SessionStart`
hooks, which is why that file carries three hook pairs instead of two, and why
`resume.err` holds an unrelated workspace-trust warning. Both are environmental
noise, not part of the contract.

## Facts these recordings pin down

- `result` lines carry `result` as a **string**, `total_cost_usd` as a **number**,
  and report outcome through `subtype` (`success` / `error_max_turns` /
  `error_during_execution`) plus `is_error` (bool). There is no `success`,
  `exit_code` or `error` field.
- `permission_denials` is present but empty in every recording. Its element
  shape is `{tool_name, tool_use_id, tool_input}`, verified separately by
  `TestExecutor_Execute_RealCLIGate`, which actually denies a tool call.
- `rate_limit_event` messages and `system` messages with subtypes
  `hook_started`, `hook_response` and `thinking_tokens` arrive mid-stream and
  must not end the turn.
- Assistant `usage` is per-request; the authoritative per-run totals come from
  the final `result` line's `usage`.
