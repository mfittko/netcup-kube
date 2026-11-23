# 0001. Let the agent merge drain PRs under a standing authorization

## Status

Accepted — 2026-11-23 ([PR 114](https://github.com/mfittko/netcup-kube/pull/114))

## Context

Every merge in this repo is a manual click in GitHub. The dev-loops extension default `autonomy.humanMergeOnly: true` applies, and the tracked `.devloops` did not override it, so the merge wrapper refused every agent merge. The operator `mfittko` asked for agent merges in [issue 113](https://github.com/mfittko/netcup-kube/issues/113).

The dev-loops merge-preconditions contract allows a recorded standing authorization ([ADR 0050](https://github.com/mfittko/dev-loops/blob/main/docs/decisions/0050-agent-merge-on-full-gate-pass.md)). Configuration alone authorizes nothing: the authorization must live in an accepted decision record that names its condition. Escalated merges need a fresh per-merge approval, which the ask-dialog approval provides ([ADR 0315](https://github.com/mfittko/dev-loops/blob/main/docs/decisions/0315-ask-dialog-merge-approval-record.md), rule `MERGE-ASK-APPROVAL`).

## Decision

The operator `mfittko` grants a standing merge authorization for drain merges in this repository. The dev-loop orchestrator (main agent) may merge a drain PR through the sanctioned merge wrapper with `--standing-authorization` when all of these hold at the current head:

- a clean `draft_gate` verdict and a clean `pre_approval_gate` verdict;
- zero unresolved review threads;
- a green gate-evidence audit;
- green CI.

A gate-incomplete PR stays unauthorized. A drain merge is gate-clean, within the size budget and touches no T1 path. Subagents never run the merge wrapper.

The authorization does not cover escalated merges: a size outcome of `escalate` or `block`, a T1-touching change, or a stable release. Each escalated merge needs a fresh per-merge approval. Under Claude Code the orchestrator asks the exact `MERGE-ASK-APPROVAL` question, and on a `Yes` answer the dev-loops hook records the approval and posts the approval line. The agent never posts that line itself.

`.devloops` sets `autonomy.humanMergeOnly: false`, which this authorization requires, and keeps the default `stopAt: [merge]`. The project `.claude/settings.json` denies agent Read, Edit, Write and NotebookEdit access to the dev-loops approval-record directory, plus a sandbox `denyRead` entry, as a second layer behind the hook.

Rejected: a standing authorization that also covers escalated merges, because the contract forbids it. Rejected: the agent posting approval lines, because the comment writers refuse that line and only the hook-posted line is agent-unforgeable.

## Consequences

Drain PRs merge without a manual click once every gate condition holds. Escalated PRs still stop for an operator answer in the ask dialog.

Rollback: a superseding record plus a `.devloops` change back to `autonomy.humanMergeOnly: true`. The merge wrapper then refuses every agent merge.
