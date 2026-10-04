---
id: phase-tracking-workflow
title: 'Phase-tracked workflow: .phase files, phase MCP tools, richer cards'
status: done
priority: high
type: feature
created_at: "2026-10-04T09:46:01Z"
updated_at: "2026-10-04T12:22:23Z"
resolution: completed
closed_at: "2026-10-04T12:22:23Z"
branch: igor.cwer/wip/phase-tracking-workflow
base_branch: main_patched
start_commit: 3253b08af539fef186e2cb92b817a69c1b15a064
final_branch: igor.cwer/phase-tracking-workflow
squash_commit: b9a3c33032e12dbb946c67a4f0ac40df6b4df244
---

Tidy up cards and MCP commands around the research → design → planning → implementation workflow. A task records who created it; each phase gets a `<phase>.phase` file (who/when started, when finished, tokens spent) written by new `start_phase` / `finish_phase` MCP tools; the board's In progress lanes are driven by those files; the wip branch is created when implementation starts. Full statement in the attached `task` file.