---
name: den-eval-review
description: Analyze a den eval export (den-eval-<user>-<time>.tar.gz from `den eval export`) and report what den does well, what it gets wrong, and concrete changes to its prompts, rules or code. Use when the user hands over an eval archive or asks how den behaved for someone.
---

# Review a den eval export

You read what den did for one user and judge it against what den is meant to do. You end with a report:
good, bad, and proposed changes, each backed by evidence from the archive. You don't change code unless asked.

The archive holds the user's prompts, whole agent conversations and parts of their code. Unpack it outside the repo,
never copy its content into the repo, and quote only the few lines that carry a point.

## 1. Unpack and orient

```sh
mkdir -p /tmp/den-eval && tar -xzf <archive> -C /tmp/den-eval
cd /tmp/den-eval/den-eval-*/
cat manifest.json
```

| Path | What |
| --- | --- |
| `manifest.json` | export time, den commit, platform, claude/codex versions, per repo: sessions, folders, calls, decisions |
| `data/repos/<repo>/den.json` | current state: folders, sessions (title, folder, progress, archived), `suggestions` = cards still waiting, `rejectedKeys` |
| `data/repos/<repo>/calls.jsonl` | every model call whole |
| `data/repos/<repo>/decisions.jsonl` | every verdict, card answer, chat action and change by hand |
| `data/repos/<repo>/prompts/<version>.txt` | the system prompt each `promptVersion` names |
| `data/repos/<repo>/folders/<id>/context.md` | den's notes per folder; `folders/repo/context.md` is the user's own repo notes |
| `data/lessons.md` | what den learned from the user's noes, shared by every repo |
| `transcripts/<tool>/<sessionId>.jsonl` | the agent conversation of each tracked session |
| `linked/`, `laptop/` | files folders link as context; the user's CLAUDE.md / AGENTS.md |

Then learn what den is *meant* to do, so you judge against intent rather than taste:

- `DEN.md` in the den repo root: behavior, model calls and their triggers, when cards may appear, what's never allowed, "Numbers to tune".
- The code wins where it disagrees with DEN.md: thresholds are constants in `internal/brain/*.go` (for example `minimumKeepConfidence` in `keep.go`); system prompts are built in `internal/brain/`; job triggers are in `internal/ui/jobs.go`.
- Check the archive's `denVersion` (manifest and each call). If it isn't the repo's current commit, run `git log --oneline <denVersion>..HEAD` so you don't propose something already changed. `-dirty` builds can't be rebuilt exactly.

## 2. How the logs fit together

**Call** (`calls.jsonl`): `id, at, job, sessionId, model, promptVersion, prompt, files, denVersion, answer, error, inputTokens, outputTokens, costDollars, milliseconds`.
Jobs: `progress review` (cheap model, title + progress every few prompts), `card review` (whole session once it goes quiet: keep / rule verdicts),
`chat`, `revise` (the user replied to a card), `context` (the user asked to change a folder's context), `lesson`, `compact lessons`, `first run`.

**Decision** (`decisions.jsonl`): `at, job, action, detail, outcome, sessionId, folderId, callId, before, after`. `callId` is the call that caused it.

| job | action | outcomes | notes |
| --- | --- | --- | --- |
| `review` | `retitle` | `applied`, `dropped: pinned`, `dropped: too long`, `dropped: same name, reworded` | detail `old → new` |
| `review` | `keep none\|notes\|folder\|skill` | `nothing to keep`, `withheld: not sure enough`, `dropped: …`, `withheld: already rejected, or enough waiting`, `proposed` | detail ends with confidence, e.g. `(0.93)` |
| `review` | `rule` | same as keep | a rule for the user's laptop files |
| `dock` | card kind: `context`, `link`, `folder`, `rule` | `accepted`, `rejected` | the user's answer to a card; `callId` = the review that proposed it |
| `revise` | `reply` | `N actions` | detail = what the user told den about a card |
| `chat` | `reply` / `plan` / an action kind (`move`, `createFolder`, `renameSession`, `editNotes`, `startSession`, …) | `proposed` / `skipped` / `applied` | a skipped plan's detail says what the user wrote instead |
| `first run` | `verdict keep\|done` | `brought in`, `left out` | den's verdict on a past session vs. the user's choice |
| `lesson` | `lesson` / `compact` | `learned`, `nothing new`, … | detail = the lesson |
| `by hand` | `rename session`, `rename folder`, `move session`, `edit file` | `applied` | the user changing things themselves; `edit file` has the whole `before` and `after` |

Following things through:

- **A card's life**: a `review` decision with outcome `proposed` and callId X, then a `dock` decision with the same callId (action `rule` for the rule card, any other action for the keep card). No dock decision: it's still waiting if `den.json` `suggestions` has a card with callId X, otherwise it vanished unanswered.
- **A correction**: a `by hand` decision on the same `sessionId` (or folder) that comes after something den applied there, like a retitle, a first-run placement, an accepted folder card or a chat action. These are the strongest sign den got something wrong.
- **What den saw**: the call's `prompt` and `files`, plus `prompts/<promptVersion>.txt`. **What it said**: the call's `answer`.
- **What really happened in a session**: its transcript. Claude lines have `type` `user`/`assistant`, `timestamp`, and `message.content` (a string, or blocks with `type: text`); skip `isSidechain` lines and user texts starting with `<`. Codex lines have `timestamp` and `payload.type == "message"` with `payload.role` and `payload.content[].text`.

## 3. Get the numbers

Run from the unpacked root. They point you at what to read; they're not the report.

```sh
R=data/repos
# what den decided, per job and action
cat $R/*/decisions.jsonl | jq -r '[.job,.action,.outcome]|@tsv' | sort | uniq -c | sort -rn
# calls, spend, latency and failures per job
cat $R/*/calls.jsonl | jq -s 'group_by(.job)|map({job:.[0].job, calls:length, dollars:(map(.costDollars)|add), medianSeconds:((map(.milliseconds)|sort)[length/2|floor]/1000), failed:(map(select(.error))|length)})'
# card answers, by kind
cat $R/*/decisions.jsonl | jq -r 'select(.job=="dock")|[.action,.outcome]|@tsv' | sort | uniq -c
# confidence of every keep and rule verdict, with its outcome
cat $R/*/decisions.jsonl | jq -r 'select(.job=="review" and .action!="retitle")|[(.detail|capture("\\((?<c>[0-9.]+)\\)$").c // "?"), .action, .outcome]|@tsv' | sort
# every change by hand
cat $R/*/decisions.jsonl | jq -c 'select(.job=="by hand")|{at,action,sessionId,detail}'
# one call whole
cat $R/*/calls.jsonl | jq 'select(.id=="<callId>")'
# everything about one session, in order
cat $R/*/decisions.jsonl $R/*/calls.jsonl | jq -c 'select(.sessionId=="<sessionId>")|{at,job,action,outcome,id,detail}' | sort
# a session's prompts, with time (claude)
jq -r 'select(.type=="user" and (.isSidechain|not))|[.timestamp, (.message.content|if type=="string" then . else (map(select(.type=="text").text)|join(" ")) end)[0:200]]|select(.[1]!="")|@tsv' transcripts/claude/<sessionId>.jsonl | grep -v $'\t<'
# a session's prompts, with time (codex)
jq -r 'select(.payload.type=="message" and .payload.role=="user")|[.timestamp, (.payload.content|map(.text // "")|join(" "))[0:200]]|@tsv' transcripts/codex/<sessionId>.jsonl | grep -v $'\t<'
```

For card answers, pair the proposal with its answer through callId to get acceptance by confidence; a handful of answers proves nothing, so say how many there are.

## 4. Read the evidence

Read whole cases, not just counts. For each, look at the call's prompt and answer, the decisions around it and the transcript near that time.

1. **Every negative signal**: rejected cards (and the lesson den drew), changes by hand after den acted, skipped chat plans, revise replies, failed calls, first-run verdicts the user overrode. For each, decide *why* den was wrong: the prompt asked for the wrong thing, the model ignored it, the input was missing what mattered, a Go check let it through, or the user just changed their mind.
2. **The silent cases**: users can't flag what den never offered. Read 3 to 5 `nothing to keep` sessions in full and judge whether a folder, notes or a rule was worth keeping. Do the same for `withheld: not sure enough`, to see whether the bar hides good cards.
3. **The positives**: a few accepted cards and applied titles. Check they're actually good (notes that don't repeat the code, folders that are real ongoing work, titles that say the work), not just tolerated.
4. **Cost and noise**: spend per session and per job against DEN.md's estimates; how often den speaks (cards per session, cards that waited for days, cards that vanished).

## 5. Report

Write the report to `den-eval-review-<user>-<date>.md` beside the archive, and give the user a short summary in chat. Plain words, no jargon the user didn't use.

```markdown
# den for <user>, <date range> (den <commit>)

<Three sentences: what kind of use this was, the single biggest problem, the single biggest win.>

## Numbers
<A small table: sessions, calls, spend, cards offered / accepted / rejected, changes by hand, failed calls.>

## Good
- <What works, with evidence: counts, and decision or call ids for the reader to check.>

## Bad
- <What went wrong, why (cause, not symptom), how often, with ids and a short quote.>

## Proposed changes
1. <The change: which prompt line, constant or check, as file:line.> Why: <the cases above it fixes>. Risk: <what it could break>. Check in the next export: <the number that should move>.

## Not enough data
- <What this export can't answer, and what would.>
```

Rank proposed changes by how much they fix for how little risk. Each one names the prompt line, constant or Go check to change, and reads its code first. Leave out changes that only restate a symptom, like "improve the prompt". Mark guesses as guesses.
