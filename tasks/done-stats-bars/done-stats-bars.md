---
id: done-stats-bars
parent_id: done-column-stats
title: 'Web: Done column renders stat cards instead of task cards; bars cards'
status: done
priority: medium
type: feature
relations:
  - type: blocked_by
    task: done-stats-service
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T19:15:25Z"
resolution: completed
closed_at: "2026-10-06T19:15:25Z"
branch: igor.cwer/wip/done-column-stats--done-stats-bars
base_branch: igor.cwer/wip/done-column-stats
start_commit: b484801d54541789479daaecd587ac92a013f769
squash_commit: 0d11246fd08289af73d3b22db5102260fe694f58
---

Give ColumnView a Stats field, set only for done, following the Lanes precedent. `_board.html` branches on it, and the Done column no longer renders task cards; the header count stays.
- Add the `_stats_bars.html` partial: a stacked done / in progress / todo bar with the last-24h part highlighted inside done, and the label `done/total · N open · +M`.
- Widths come from CSS classes or variables, so Go computes no geometry. Readable at the column's narrow width, on the dark board.
- Rebuild app.css and add an assets_test guard.
- Update the tests that expected done cards: TestBoardRendersThreeColumns, TestBoardShowsBranchChip, TestCopyButtonHasNoHtmxAttributes, TestBoardEmptyInProgressShowsFourStubs, TestLanesPartitionTheColumn.
- Add view-mapping and handler tests.
- Update the CLAUDE.md Web UI section, including the note that the final branch is now visible only in the detail view.