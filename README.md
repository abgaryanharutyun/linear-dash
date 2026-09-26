# linear-dash

A [gh-dash](https://github.com/dlvhdr/gh-dash) style terminal dashboard for [Linear](https://linear.app): tabs of issues defined by filters in a YAML file, a detail pane, and keys to open, copy, move and assign issues.

## Install

```sh
go install github.com/abgaryanharutyun/linear-dash@latest
```

## Setup

1. Create a personal API key at <https://linear.app/settings/account/security> and export it as `LINEAR_API_KEY`.
2. Copy [`config.example.yml`](config.example.yml) to `~/.config/linear-dash/config.yml` (or pass `--config <path>`).

Each section's `filter` is a Linear [`IssueFilter`](https://studio.apollographql.com/public/Linear-API/variant/current/schema/reference/inputs/IssueFilter), sent to the API unchanged, so anything the Linear API can filter on works here.

## Keys

| Key | Action |
| --- | --- |
| `tab` / `l`, `shift+tab` / `h` | next / previous section |
| `j` / `k` | move |
| `o` | open issue in browser |
| `y` | copy issue URL |
| `b` | copy Linear's git branch name |
| `s` | move issue to another workflow state |
| `a` | assign issue to me |
| `r` | refresh section |
| `q` | quit |

Copying uses OSC 52, so your terminal must allow clipboard writes (Ghostty, iTerm2, kitty, WezTerm and tmux with `set-clipboard on` do).

Logs go to `~/Library/Caches/linear-dash/linear-dash.log` on macOS (`~/.cache/linear-dash/` on Linux).
