# Instruction-based card review

The fixed policy is [card_review.md](../internal/brain/card_review.md). It applies saved user instructions, without an additional durable-knowledge eligibility rule. The separate chat prompt manages those instructions and shares the evidence, routing and confirmation principles.

## Context and routing

The reviewer starts with active scoped instructions, complete new exchanges, a compact folder index, pending proposals and relevant rejection feedback. Each index entry includes a stable identifier, path, parent, purpose and short contents excerpt. Explicit purpose comes from `## Purpose` or `## Goal`; absent purpose is unspecified. Excerpts are deterministic and cached by content hash, with no model call per exchange.

Full contexts are staged as `context-<identifier>.md` files. Each contains the destination, ancestors and a `readVersion`. A proposed write must supply that version. The index cannot authorize rewriting or establish novelty. New folders read the parent's snapshot, including repo context for a top-level folder.

An instruction may also request global coding-agent preferences. These use the existing rule cards, with the current supported global rule files staged in `global-rules.md`. Only an explicit user preference applying across projects qualifies. The proposal contains one exact addition and its affected files, requires acceptance, and is revalidated if those files change. Existing rules are never rewritten by this action. Reusable workflows are preserved in ordinary folder context for future sessions.

Folders can serve any purpose. Instructions do not own folders, and observation scope does not choose a destination. Prefer a fitting existing folder, follow explicit placement preferences, and propose a reusable folder only when an actual finding has no suitable destination. New instruction-based folders leave source sessions in place.

## Validation and maintenance

Every proposal names its source instruction and revision, destination, supporting quotes, reason and exact change. Validation rejects unknown/inactive instructions, changed revisions, evidence absent from the supplied exchanges, unsupported confidence, unknown destinations and stale snapshot versions. Notes have no three-line truncation. An exact unique `replacesText` passage allows status corrections, resolutions, disproofs and removal of obsolete text while preserving unrelated content.

Acceptance applies the visible change. Changes to destination or inherited context require revalidation. Pending legacy proposals remain reviewable; the old automatic plan/link/skill proposal rules no longer create new cards. Existing explicit chat actions for agent rules and linked context remain separate from Den instruction management.

Five proposals can be visible. Additional proposals persist in the backlog and are revalidated before promotion. Revalidation must read the current destination snapshot. Per-card discussions, saved drafts, rejection reasons and scoped destination corrections remain available. Pausing an instruction does not automatically withdraw its existing cards.

## Instructions and chat

The **Instructions** row opens a list in the existing right panel. Enter edits the selected instruction; `n` creates one. The form separates Title, Instruction, and Examples, with Tab moving between fields. Typing and pasted text save automatically. Incomplete edits persist as drafts while the last valid active instruction remains unchanged. Conflicting edits stay as drafts instead of overwriting external changes; `x` discards a draft after confirmation. Space pauses/resumes an instruction; `d` deletes after confirmation.

When leaving a changed instruction with missing or stale examples, Den proposes illustrative examples through the existing chat confirmation flow. Ctrl+G also requests examples, and `c` opens instruction chat. Generated examples never save without approval. The instruction itself remains saved if the model is unavailable. `m` opens the underlying Markdown editor; Alt+O opens an external editor. Built-in file editors autosave; invalid raw Markdown is preserved with a validation message until corrected. External editors retain their own save controls. Instructions remain outside coding-session context.

An empty instruction panel offers four optional starters: issues and their status, explicitly postponed work, reusable workflows, and global coding-agent preferences. Space selects each behavior; Enter enables only the selected instructions together with their displayed illustrative examples. Existing instructions are preserved, and `s` reopens the chooser for missing starters. Starter matching checks stable identifiers, titles, and identical behavior text to avoid duplicates.

Instruction-panel chats receive no history files. General chat is explicitly forbidden from retrieving historical examples for setup. Changed behavior/scope invalidates old example approval fingerprints and permits direct review until examples are approved again. Instruction revision fingerprints also include title and examples for proposal provenance.

## Evaluation

See [the feature report](../experiments/instructions/RESULTS.md) for historical relabeling, embedding comparisons, reviewer routing/lifecycle scenarios, chat flow, local safeguards, cost and latency. Synthetic scenario checks establish those behaviors; they are not an estimate of production proposal accuracy. The repository's existing single-writer state model remains: multiple panels writing the same repo are not coordinated by this feature.
