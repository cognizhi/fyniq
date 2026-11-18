# The visual snapshots (QA-UI-07, QA-RPT-04)

`make test-visual` draws the key pages of the sample company (Home, the Inbox, a financial statement, an invoice and
Settings) at the five reference frames of §7.10, in light and dark, and compares each picture's SHA-256 with
`baselines.json` (p1-launch-readiness design D5). CI runs it on every pull request, in the images job.

- **A stack of their own.** `scripts/visual.sh` starts the stack as the `fyniq-visual` project on ports of its own,
  with the business date fixed at 31 Dec 2026, sets it up, and makes the organisation "Visual Pte Ltd" with the sample
  company. The browser's clock is fixed too, motion is reduced and the caret hidden.
- **The pinned container.** Playwright runs in its own Linux image, pinned by digest in the Makefile, so the browser,
  its fonts and libraries never change between runs. The baselines are linux/amd64's, as CI draws them; elsewhere
  `VISUAL_PLATFORM=linux/amd64` draws them under emulation.
- **A changed picture fails the run.** The run leaves the pictures that differ, and every hash it saw, in
  `test-results/visual/` (CI's artefact `visual-diffs`). Look at them; if the change is meant, take the hashes in with
  `python3 scripts/visual.py approve <the artefact's folder>` and open the pull request with the label
  `visual-baseline`. CI refuses a change to `baselines.json` without it.
- **Every route** (`VISUAL_ALL=1`): each signed-in route of the registry whose path names no record, drawn nightly
  (`.github/workflows/visual.yml`) and before each release, where a changed picture builds no image (`release.yml`).
- **Until the first baselines are approved**, a run only records its hashes.
