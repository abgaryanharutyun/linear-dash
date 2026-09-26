# linear-dash

A [gh-dash](https://github.com/dlvhdr/gh-dash) style terminal dashboard for [Linear](https://linear.app): tabs of issues defined by filters in a YAML file, a detail pane, and keys to open, copy, move and assign issues.

## Install

```sh
go install github.com/abgaryanharutyun/linear-dash@latest
```

## Setup

1. Create a personal API key at <https://linear.app/settings/account/security> and export it as `LINEAR_API_KEY`.
2. Copy [`config.example.yml`](config.example.yml) to `~/.config/linear-dash/config.yml` (or pass `--config <path>`).

Sections are either an issue list (`filter`, a Linear [`IssueFilter`](https://studio.apollographql.com/public/Linear-API/variant/current/schema/reference/inputs/IssueFilter) sent to the API unchanged) your inbox (`kind: notifications`), or releases (`kind: releases`: planned and in-progress releases plus those completed in the last `recentDays`; `enter` opens a release's issues). Any section can set `grouped: true` to open grouped by state (pipeline for releases); `t` toggles it. `refreshMinutes` reloads everything on a timer, and `repoPaths` maps a Linear team key to the local clone that `B` checks branches out in.

Each row's second line shows the team and the most relevant linked PR with its status. The detail pane shows the rendered description, comments, sub-issues and linked GitHub PRs with their state, checks and review status (PR status needs the [`gh`](https://cli.github.com) CLI, logged in).

## Keys

| Key | Action |
| --- | --- |
| `tab` / `l`, `shift+tab` / `h` | next / previous section |
| `j` / `k` | move |
| `enter` / `esc` | open a release's issues / go back |
| `/` | filter the section (enter keeps it, esc clears) |
| `t` | group by workflow state |
| `o` | open issue in browser |
| `O` | open the linked GitHub PR (picker when there are several) |
| `y` | copy issue URL |
| `b` | copy Linear's git branch name |
| `B` | check out the issue's branch in the team's repo (creates it if needed) |
| `s` | move to another workflow state |
| `a` / `A` | assign to me / to a teammate |
| `p` | set priority |
| `e` | set estimate |
| `L` | edit labels (tab toggles) |
| `c` | comment (ctrl+s sends) |
| `m` | mark notification read (inbox sections) |
| `r` | refresh section |
| `q` | quit |

Pickers filter as you type; move with `↑`/`↓` or `ctrl+n`/`ctrl+p`.

Copying uses OSC 52, so your terminal must allow clipboard writes (Ghostty, iTerm2, kitty, WezTerm and tmux with `set-clipboard on` do).

Logs go to `~/Library/Caches/linear-dash/linear-dash.log` on macOS (`~/.cache/linear-dash/` on Linux).
