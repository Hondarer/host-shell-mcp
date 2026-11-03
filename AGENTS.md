# AGENTS.md

Instructions for coding agents working in this repository. The design rules in [docs/en/development.md](docs/en/development.md#design-rules) also apply (the [Japanese version](docs/ja/development.md) is authoritative).

## Documentation policy

Documents describe the current, final specification. History belongs in `docs/prompts/past/`.

This applies to `docs/ja/`, `docs/en/`, `README.md`, `docs/README.md`, and every `SKILL.md`.

### What documents contain

- **Use cases**: what the feature is for and when to use it.
- **Procedures**: the steps, options, and commands to use it now.
- **Verified scope**: what has been verified, with the method (automated test, CI, container, real machine) and the environment (OS, CPU, libc, product versions). List unverified items explicitly. Write the status as 検証済 / 未検証 (Verified / Unverified), not 成功 or Passed.
- **Limitations**: what is out of scope and known constraints.

### What documents do not contain

- Dates of work, who confirmed what ("the user confirmed", "observed in a screenshot"), or chronological narratives ("then", "subsequently").
- Log excerpts, timestamps, and investigation steps.
- Corrections of earlier text ("an earlier note was wrong") or descriptions of superseded behavior.

Dates and numbers that are part of the specification stay: supported versions, measured values with their environment, and example values.

### Updating

- When a result changes, rewrite the specification in place. Do not append a dated section.
- Record the history of a task (discussions, designs, backlogs, validation records) in `docs/prompts/past/yymmdd-<topic>-<kind>.md`, in Japanese only. `<kind>` is `discussion`, `design`, `backlog`, or `validation`. A document's verified-scope section may link to the relevant record.
- Records in `docs/prompts/past/` are snapshots. Do not update them to match the current state.
- Edit the Japanese version (`docs/ja/`) first and reflect the same change in `docs/en/` in the same commit.
