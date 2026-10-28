# Card review, feedback and discussions

DEN preserves information that helps future sessions make better decisions or avoid repeating meaningful work. Sessions are temporary; folders are continuing workspaces. Repo, parent and child context compose from broad knowledge to specific instructions. A nested skill adds a method at the scope where it applies, without repeating its parents.

The live Sol policy is [internal/brain/card_review.md](../internal/brain/card_review.md), embedded by `review_policy.go`. Edit that file to change reviewer behavior. The review and card-discussion prompts share its mental model. The [classifier gate](classifier-gate.md) still uses the deployed ModernBERT SetFit encoder, logistic head and 0.625 threshold; this change does not retrain it.

## What earns a proposal

Sol receives the folder tree with stable identifiers, full destination contexts, source exchanges, waiting proposals and recorded rejection feedback. For each proposal it must identify supported information, its destination, what is missing from inherited context, and a concrete future task where retaining it changes an action or prevents rediscovery. Specific constraints and durable rationale can qualify after implementation. Routine status and temporary test output generally do not.

For model-proposed keeps, Go requires confidence of at least **0.7**, a reason, a future-use explanation and nonempty evidence. Notes contain at most three related additions. A correction can name one exact existing destination passage to replace. Supporting quotes are retained but are not mechanically verified against the transcript; semantic usefulness, novelty and accuracy remain reviewer judgments. Existing local approved-plan and written-file triggers remain; those cards show their recorded reason when no model-written future-use explanation exists.

Notes can target any existing folder or repo context, including discoveries from Inbox. The source session stays where it is. New-folder cards can move their source sessions only after acceptance. Existing folders with context receive methods through notes proposals; `skill/place: here` is reserved for an empty folder. Global rules require an explicit cross-repository preference, not an inferred preference from one task.

## Visible cards and saved backlog

At most five cards are visible. Additional valid proposals persist in `den.json` with `deferred: true`. There is no per-session or per-kind proposal limit. Context and link edits for the same destination become visible sequentially to avoid competing rewrites.

Before a queued proposal appears, Sol checks it against current destination and inherited context, recent source exchanges and rejection feedback. The response is discarded if the proposal revision or context/source fingerprint changed while the call ran. Additive notes are rebuilt on the latest context and skip exact inherited duplicates. Whole-context rewrites need a refreshed complete text. If the file changes after display, acceptance refuses a stale rewrite or refreshes an additive proposal and requires a second confirmation.

Revalidation failures leave proposals queued, with a one-minute retry delay. Invalid proposals are withdrawn and logged. If a proposal has a conversation, it returns with the concern instead; acceptance is blocked until it is revised. Automatic card review also preserves cards being discussed or actively reviewed, with its concern blocking acceptance until revised. Explicit replacements retain the card identity, conversation, draft, position, source sessions and gate-call provenance; matching kind alone never replaces a card.

## Using the card

The card separates the proposed change and full destination, future usefulness, source, evidence and conversation. Notes show additions and removals. Questions and replies stay attached to the proposal even when another card is open.

| Key | Action |
| --- | --- |
| Tab | Open the card dock; leave it when focused |
| Enter | Send typed text, or accept when the input is empty |
| Backspace, empty input | Open the required rejection-reason form |
| Ctrl+D | Choose another destination for the proposal |
| Ctrl+R | Start a revision request |
| Ctrl+E | Expand or collapse supporting evidence |
| Ctrl+J | Jump to the latest conversation and clear its unread marker |
| PgUp / PgDown, Shift+Up / Shift+Down, touchpad | Scroll the card and discussion |
| Up / Down, empty input | Switch cards; choose an option inside a form |
| Escape | Cancel a form or leave the card |

Moving or revising a proposal does not write context or move its source sessions. It updates the draft proposal, which still needs acceptance. One discussion answer can revise one proposal; unrelated or multiple actions are not applied. Questions, replies, input drafts, evidence expansion and scroll positions persist across navigation and restarts. A stopped answer preserves the question and asks for a retry. New replies remain unread until Ctrl+J opens the latest conversation.

## Feedback and future evaluation

Rejection requires one of these reasons:

- Not useful for future work
- Already covered
- Incorrect or unsupported
- Wrong destination
- Too detailed or poorly written
- Something else

An additional explanation is optional except for “Something else.” The reason and explanation are saved with the full proposal, conversation, evidence, source sessions, reviewer identifier and triggering gate identifiers. Destination corrections also retain the old proposal and new destination. Rejection logging must succeed before a card is removed.

Exact rejection keys identify information and destination. They no longer include source-session bans; legacy broad session keys are ignored. Every Sol card review requires a short factual outcome reason, including why it returns no new proposals or withdraws a card; this is retained in the calls and decision log. Sol also reads rejection feedback to avoid resurfacing the same rejected information through rewording. Wrong destination or poor writing is not a negative label for underlying usefulness. General lessons remain in `~/.den/lessons.md`, within the review policy rather than overriding it.

| Storage | Contents |
| --- | --- |
| `~/.den/repos/<repo>/den.json` | Visible and deferred proposals, per-card discussion state, rejection feedback |
| `decisions.jsonl` | Proposal snapshots, replacements, withdrawals, moves, revisions, revalidation, acceptance, rejection and discussion outcomes |
| `calls.jsonl` and `prompts/` | Full model requests and responses, prompt versions, usage and call identifiers |

Follow `gate call → card review → suggestion → decisions/revisions/revalidation`. The existing `den eval export` includes this state and these logs. Do not treat a rejection as an automatic negative gate label: duplication, destination and wording are separate problems. Build new training labels from reviewed evidence, retain conversation-group splits, and keep a fresh held-out set. The old classifier accuracy is not a measurement of this new reviewer/UI behavior.

## Implementation and verification

- `internal/brain/keep.go` validates model proposals and resolves their destinations.
- `internal/store/suggestions.go` and `proposals.go` handle identity, feedback, persistence, safe context preparation and acceptance.
- `internal/ui/proposal_backlog.go` schedules and applies revalidation.
- `internal/ui/card_revision.go` updates proposals without applying them.
- `internal/ui/dock.go` renders the card, forms and persistent conversation.

Local rollout artifacts are kept in ignored `.local/card-rollout/`: the temporary scenario checks, their results, terminal render fixtures, and synthetic Sol requests/responses. They contain the initial nested-skill mismatch as well as its corrected rerun. The focused checks cover rejection scope, backlog persistence, sequential rebasing, stale-context acceptance, inherited duplicates, cross-folder routing, saved drafts/discussions, late replies, destination moves and narrow/wide rendering. These checks and the synthetic prompt cases establish behavior, not a new accuracy benchmark.

Old context proposals without a context-version fingerprint enter the backlog on load for revalidation. Existing panels keep their loaded binary; restart the DEN panel to use a new build. The repository's existing single-writer state model is unchanged; simultaneous panels writing the same repo are not coordinated by this change.
