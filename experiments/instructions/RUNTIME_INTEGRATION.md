# Instruction editor and trained gate integration — 2026-11-01

The user's interim choice, fine-tuned MiniLM using only the new exchange, is now connected to Den's local gate. Integration did not retrain weights, change the input layout, or tune the threshold. This is a personal rollout with known limitations, not a new claim of generalization.

## Active model

- Checkpoint: `trained-new-epoch-3`, 22,713,601 parameters.
- Execution: CPU FP32, four Torch threads, persistent offline Python worker.
- Threshold: `0.005811875686049461`.
- Instruction plus approved examples: first 160 tokens. New exchange: 256-token windows with 64-token overlap. Pair encoding: at most 512 tokens. Maximum sigmoid score across windows.
- Artifact hashes: [trained_release.json](trained_release.json). The installer refuses a different checkpoint. Private weights are not committed or downloaded from an unrelated public model.
- Installation: `${DEN_HOME:-$HOME/.den}/instruction-gate`. Missing approved examples, pending-card maintenance, or unavailable inference allow direct Sol review.

The historical [cross-encoder report](CROSS_ENCODER_RESULTS.md) remains the source for model selection, labels, and limitations. At this threshold the model skips 37/160 historical exchanges (23.1%) and catches 86/88 relevant exchanges (97.7%). Individual-instruction recall is weaker: issues 81/85, deferred work 2/3, architecture 33/33, and API constraints 22/25. The four starter behaviors are not identical to these evaluation instructions; these figures do not establish their recall or production cost savings.

## Integration verification

The installed worker was replayed against the original 160 historical exchanges and four instructions, with the same ordering and frozen threshold. All **640 scores matched the saved reference exactly**: maximum absolute difference 0, no changed pair decisions, 37 skipped exchanges and two combined misses. Primary labels are the original experiment's `test-clarified` labels, with 88 positives. The older raw labels contain 84 positives and are not the primary denominator.

This replay took 12.78 seconds including worker startup on the development machine. It reuses an inspected dataset and verifies integration parity; it is not a fresh held-out evaluation. No new historical Sol review was run.

The existing Go replay checks passed:

- 24 local instruction, scope, evidence, revision, context, and confirmation checks.
- Cold/warm worker use, detecting a finding within a long exchange, and direct-review fallback after intentionally corrupting the isolated manifest.
- Gate replay of 16 existing reviewer scenarios; no scenario requiring a change was blocked. Reviewer quality itself was measured in earlier reports.

`go build`, `go vet ./...`, Python source parsing, and `git diff --check` passed. Private outputs are under `.local/trained-instruction-runtime`, `.local/instruction-trained-gate-checks`, and `.local/instruction-form-checks`.

## Editor

Instructions use the existing right panel, with a list and one New/Edit form. Title, Instruction, and Examples have separate fields. Typing and paste autosave; incomplete or conflicting edits remain recoverable drafts without replacing the last valid instruction. Space pauses/resumes; deletion and draft discard require confirmation. Markdown and external editing remain available.

Den proposes missing or stale examples on leaving a changed valid instruction, or when asked with Ctrl+G. Generated examples wait for acceptance. A live isolated UI check verified that three suggested examples were absent from the saved file before approval and present afterward, with the original instruction unchanged. The first sandboxed model call failed on CLI filesystem permissions; repeating the isolated check with the authenticated CLI outside that sandbox succeeded.

Manual tmux checks covered 120×40 and 80×30 layouts, creation, automatic saving, pause/resume, and restoring an incomplete draft after restarting Den. Earlier autosave checks also covered paste, external-edit conflicts, and starter selection. Fresh repos offer optional starters; the user's existing five repo stores were populated separately at their request, preserving an existing workflow instruction.

The trained runtime was installed locally after verification. New Den processes use it; existing processes require a restart to load the updated executable. Remote release and Homebrew publication are separate from the local merge.
