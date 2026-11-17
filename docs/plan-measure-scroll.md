# Plan: `make measure-scroll`, scroll smoothness measured on a real phone

Branch: `measure-scroll` (dev tooling, not a slice; merged with `--no-ff`). This plan is committed there and deleted before the merge.

## Problem

- The group list scrolled at 36 fps on a Galaxy S10 in Firefox (fixed by `232db4b`: stub mask-image, card box-shadow). Desktop Chromium, even with CPU throttling and an emulated S10 screen, stayed at 60 fps with or without each property: only the phone showed the cost, and the eye could not rank variants.
- The ad hoc tool that found it (static snapshot of the list, scripted scroll, results sent back to the dev machine) lived in a temp directory. Goal: keep its reusable core, a before/after of a CSS change measured on a phone, nothing more.

## Usage

```
make measure-scroll [REF=main]
```

1. Prints `on wlp194s0: open http://192.168.1.28:8765/ on the phone (plugged in, battery saver off), tap "Start", don't touch the screen`.
2. On the phone: one tap; 8 runs of about 6 s, the page reloading between them.
3. The terminal prints one line per run received, then the table, and exits:

```
Firefox 160 on Android, refresh 16.7 ms, 4 pairs
           fps                  dropped frames
ref        36.2                 38 %
work       58.7                  2 %
work-ref   +22.5 [+21.0 +24.1]  -36 pts [-38 -33]
```

## Design

- **Compares CSS only**: `ref` is `internal/web/static/style.css` at `REF` (`git show`), `work` is the working tree's, both on the working tree's HTML. A template change is not compared; stated in the script header.
- **Static snapshot** of the group page, not the live demo: its CSP forbids the injected benchmark, and a snapshot is stable across runs.
  - `measure-scroll.mjs snapshot` with `playwright-core` (installed for `verify-ui`): join as Léa, open `details`, clone the cards ×5 (~165 cards), drop `<script>` and icon links, mark the stylesheet and the benchmark script for the server to fill. The demo is stopped right after.
- **Served on the LAN** by `measure-scroll.mjs serve` (Node `http`, no new dependency):
  - Address of the default route (`ip -4 route get 1.1.1.1`, no packet sent), printed with its interface; with a full-tunnel VPN it is the VPN's, a limit stated in the header. No override, port fixed at 8765 (history completes the URL); a taken port fails clearly.
  - A fixed map of routes, never a directory: `/` (start page), `/?run=i` (the snapshot with the CSS of run i), `/ref.css`, `/work.css`, `/bench.js`, `POST /results` (body capped at 64 KB). Every response `Cache-Control: no-store`.
  - **The server drives the series**, no `localStorage`: order ABBA BAAB (ref/work), fixed against thermal drift; run i's page gets `data-run`, `data-nonce` (per session) on its script tag. A result with another nonce is rejected.
  - Exits after the last run with the table; on Ctrl-C prints the table of the runs received so far. Each pair (runs 2k, 2k+1) gives a work−ref difference: median and min–max shown, so noise is visible.
- **Benchmark in the page** (`scripts/measure-scroll-page.js`, served as is, kept minimal; all statistics in Node):
  - Scrolls to 300 px (the dynamic toolbar settles), waits 1.5 s, then scrolls by elapsed time (1500 CSS px/s) for 4 s, recording rAF intervals; posts `{nonce, run, ua, dt}` with `navigator.sendBeacon` (Firefox cancels a `fetch` when the page navigates right after: a code comment says so), then goes to `/?run=i+1`.
  - Run spoiled by `visibilitychange` or `pointerdown`: not posted, the same run reloads. Bottom reached: posts an error, the server stops with "list too short".
  - Header states what rAF intervals measure: main-thread work plus WebRender backpressure (how the mask and shadow showed); not what only a finger (APZ) scroll hits, such as checkerboarding; main-thread costs are overstated.
- **Metrics** (Node): refresh interval = 10th percentile of the intervals; fps; dropped frames = Σ(round(Δ/refresh) − 1) / Σ round(Δ/refresh), valid at 60, 90 or 120 Hz.
- **Shared with `verify-ui`**: `scripts/ui-lib.sh` with `ensure_tools` (all three tools, one stamp), `start_demo` and `stop_demo`, moved from `verify-ui.sh`. `start_demo` creates `$work` and owns the only EXIT trap (globals, no `set -e` in the lib; said in its header).

## Changes

1. `scripts/ui-lib.sh`, and `scripts/verify-ui.sh` sourcing it (behaviour unchanged).
2. `scripts/measure-scroll.sh`: `ensure_tools`, `start_demo`, `ref.css` from `REF`, snapshot, `stop_demo`, serve (foreground, so Ctrl-C reaches it).
3. `scripts/measure-scroll.mjs` (`snapshot`, `serve`), `scripts/measure-scroll-page.js`.
4. `Makefile`: `measure-scroll` (`REF ?= main`), in `.PHONY`. No other knob.
5. Docs: `CLAUDE.md` (Structure `scripts/` list, Commands), ADR 0006 (a bullet: `measure-scroll`, Node only, manual, needs a phone on the same network, not run before merges).

## Verification

- `make verify-ui` gives the same output after the `ui-lib.sh` move.
- `make measure-scroll` end to end with headless Chromium standing in for the phone (throwaway driver in the scratchpad): table printed, exit 0. Then Ctrl-C mid-series (partial table) and port 8765 taken (clear error). After each: `pgrep -f "bin/concerts [s]erve"` empty, `ss -ltn` without 8765.
- Once on the S10: `REF=a713621` (before `no-fade`) shows ~36 vs ~59 fps again.
