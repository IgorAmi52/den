<div align="center">

<img src="docs/images/banner.svg" alt="den: an agent for your agent sessions" width="100%" />

### 🛖 Run *Claude Code* & *Codex* side by side, ask where you left off, start every session with the right context 🛖

[![platform](https://img.shields.io/badge/platform-macOS-lightgrey)](#-quickstart)
[![install](https://img.shields.io/badge/install-brew-orange)](#-quickstart)
[![agents](https://img.shields.io/badge/agents-Claude%20Code%20%7C%20Codex-8A63D2)](#-quickstart)
[![status](https://img.shields.io/badge/status-early-yellow)](#-features)

</div>

---

<p align="center"><img src="docs/images/den-demo.gif" alt="den demo: asking where I left off on Friday, opening a session that picks the work back up, opening three more agents in other folders, and switching between them with Option-Tab." width="100%"></p>

<p align="center"><sub>Ask about Friday → den writes a handoff → a new agent already knows where things stand → open three more in other folders → <kbd>⌥</kbd><kbd>⇥</kbd> between them. Keys pressed are shown under the window. <a href="docs/den-demo.mp4">MP4</a></sub></p>

## ✨ Highlights

<table>
<tr>
<td width="33%" valign="top">

🧠 **Remembers every session**<br>
<sub>goal · done · next, kept up to date while you work</sub>

</td>
<td width="33%" valign="top">

🔁 **Picks up where you left off**<br>
<sub>ask *"where did I leave off?"*, get a new agent with a handoff</sub>

</td>
<td width="33%" valign="top">

🪟 **Four agents, one window**<br>
<sub><kbd>⌥</kbd><kbd>⇥</kbd> to cycle · <kbd>⌥</kbd><kbd>1–4</kbd> to jump · `!` when one needs you</sub>

</td>
</tr>
<tr>
<td width="33%" valign="top">

📁 **Context per folder**<br>
<sub>every session starts with its folder's notes · yours, never committed</sub>

</td>
<td width="33%" valign="top">

✦ **Keeps what matters**<br>
<sub>suggests notes and folders from finished sessions · <kbd>↵</kbd> or <kbd>⌫</kbd></sub>

</td>
<td width="33%" valign="top">

🔒 **Never touches your code**<br>
<sub>no commands, no git · nothing written without a yes</sub>

</td>
</tr>
</table>

## 🚀 Quickstart

Install [Homebrew](https://brew.sh) first if it is not already available.

**Release status:** the formula currently installs `v0.2.0`. The local classifier and persistent card discussions described below are newer, unreleased changes. They become available through Homebrew after a new release updates the formula; `--HEAD` follows published `main`, so it does not include unpublished work.

```sh
brew tap IgorAmi52/den https://github.com/IgorAmi52/den
brew install den

cd your-repo
den
```

- **Needs:** macOS, Git, and [Claude Code](https://docs.anthropic.com/en/docs/claude-code) and/or [Codex](https://github.com/openai/codex) on your `PATH`. Homebrew installs tmux and the Go build dependency. If Git is missing, run `xcode-select --install`.
- **Login:** open your installed agent CLI once and finish its login before starting DEN. DEN prefers Codex when both CLIs are installed; an installed but logged-out Codex does not automatically fall back to Claude. The current source uses `gpt-6-luna` and `gpt-6.1-sol`, so your Codex account must have access to them. Claude-only installations use Haiku and Sonnet.
- **First run:** press <kbd>⌥</kbd><kbd>L</kbd> so den can check your Option key (it fixes Terminal.app and Ghostty for you) · tick which of your 10 newest past sessions to bring in
- **Coming back:** run `den` in the repo again. Closing the terminal doesn't stop your agents.

<details>
<summary><b>Build from source, install the classifier, or upgrade</b></summary>
<br>

**Source build.** Requires Go 1.26 or newer, tmux, Git and a logged-in supported agent. Run these in a checkout containing the version you want:

```sh
brew install go tmux
make install
export PATH="$HOME/.local/bin:$PATH"
command -v den
```

Add that `PATH` line to `~/.zshrc` to keep it in new terminals. Source installs use `~/.local/bin/den`; Homebrew installs use its own prefix. Check `command -v den` when switching between them. `make install` builds first, creates the destination directory, then safely replaces the binary; a failed build leaves the installed version intact. A custom destination is supported: `make install INSTALL_DIRECTORY="/your/bin"`.

**Local classifier, in the current source.** This is a separate optional installation. Homebrew and a Git clone do not include the trained ModernBERT files. Without them, DEN logs the missing runtime and sends exchanges directly to its model reviewer. Suggestions still work, with more model calls and potentially higher cost; Python is not required in that mode. The classifier runtime has been verified on Apple silicon macOS. Its pinned Python stack is not supported on Intel Macs; direct reviews remain available there. To install it you need `uv`, the separately provided model artifacts and several gigabytes of free space. Follow [classifier installation](docs/classifier-gate.md#install-or-restore). There is no public model download yet.

**Upgrades.** Use `brew update` and `brew upgrade den` for a released version, or rebuild your source checkout. Existing panels keep running their old binary; merely reattaching to tmux does not reload it. Restart DEN after your agents have finished: **`q` closes the entire DEN window, including its agent panes.** Saved context, cards and discussions remain in `~/.den`. Back up that directory before downgrading; older versions may discard state fields they do not understand.

`DEN_HOME` changes where all DEN state and the classifier runtime live. Use the same value for installation and launch. Keep one DEN panel active per repository; panels across its worktrees share the same state and do not coordinate simultaneous writes.

</details>

## ⌨️ Keys

| Anywhere in den's window | |
| --- | --- |
| <kbd>⌥</kbd><kbd>⇥</kbd> | **next pane** · den → agent 1 → … → den |
| <kbd>⌥</kbd><kbd>1</kbd>–<kbd>4</kbd> | jump to an agent |
| <kbd>⌥</kbd><kbd>0</kbd> | jump to den, and back |
| <kbd>⌥</kbd><kbd>N</kbd> | new agent |
| <kbd>⌥</kbd><kbd>=</kbd> | default pane sizes, also a double click on a border |

| In den | |
| --- | --- |
| <kbd>c</kbd> | **ask den** |
| <kbd>n</kbd> / <kbd>N</kbd> | new session in the selected folder, with / without its context |
| <kbd>↵</kbd> | resume a session · focus an agent |
| <kbd>⇥</kbd> | den's suggestions: <kbd>↵</kbd> accept · <kbd>⌫</kbd> reject with reason · or type |
| <kbd>→</kbd> | a folder's context: <kbd>↓</kbd> ask den to change it · <kbd>↑</kbd> edit by hand |
| <kbd>z</kbd> | wide view with a preview |
| <kbd>f</kbd> <kbd>F</kbd> · <kbd>m</kbd> <kbd>r</kbd> <kbd>a</kbd> <kbd>d</kbd> | folder, subfolder · move, rename, archive, delete |
| <kbd>/</kbd> · <kbd>?</kbd> · <kbd>q</kbd> | filter · all keys · quit |

Marks: `●` working · `!` needs you · `✓` your turn · `◆` has context · `✦` den has a suggestion

## 🧭 How to think with den

- **One task, one session.** Let sessions go. They archive themselves 3 days after you last used them, and stay resumable.
- **Context is what lasts.** A folder's context is a short markdown file every session started in it begins with. Folders nest, and context adds up: `repo → Checkout → Coupons`.
- **Two kinds of folders.** A piece of work (`Checkout → Coupons`: what's decided, what's open) or a way of working (`Exploration`: *research only, cite file and line*). Folders are den's own labels, not directories.
- **Don't write context, ask for it.** *"make the context from the latest session"*, *"add that we decided X"*, or accept one of den's suggestions.

## 📖 Features

<details>
<summary><b>💬 Ask den</b> · recall, resume, organize</summary>
<br>

<img src="docs/images/recall-and-handoff.png" alt="den's chat: where did I leave off on Friday, then a plan card with a handoff for a new session." width="760">

<kbd>c</kbd> opens a chat about the whole repo. den answers from its notes on every session, and reads full conversations when it needs to. Changes come as a plan that waits for <kbd>↵</kbd>.

| | Try |
| --- | --- |
| **Recall** | *where did I leave off on Friday?* · *what did we decide about pagination?* |
| **Resume** | *open a new session that picks up the coupon work* |
| **Organize** | *make an Exploration skill folder: research only* · *move the search session into Email Search* · *link docs/PLAN.md as Eval's context* |

A handoff covers where it stands, decisions, what's next and the files involved. It goes into the new agent's system prompt.

</details>

<details>
<summary><b>📁 Folders & context</b> · what every new session starts with</summary>
<br>

<img src="docs/images/context-change.png" alt="Coupons' context with den's proposed change shown as a diff." width="760">

<kbd>→</kbd> on a folder opens its context. <kbd>↓</kbd> asks den to change it, and you get a diff to confirm. <kbd>↑</kbd> edits it by hand (<kbd>⌥</kbd><kbd>O</kbd> opens `$EDITOR`). To use a file of your own instead, like `docs/PLAN.md`, tell den *"link docs/PLAN.md to Coupons"*. The `◆ context` row at the top is the repo's, and every session starts with it.

</details>

<details>
<summary><b>✦ Suggestions</b> · den notices what's worth keeping</summary>
<br>

<img src="docs/images/suggestion-card.png" alt="A FOLDER card with the context den drafted and the quotes it's based on." width="760">

After a completed reply, a local classifier decides whether Sol should review it for useful context. Plans, markdown writes and pending cards always get a review; there is no hourly review cap. See the [classifier setup and behavior](docs/classifier-gate.md). When something is worth keeping, a card waits at the bottom of den: <kbd>↵</kbd> accept · <kbd>⌫</kbd> reject with a reason · or type a question or revision.

| Card | Yes does |
| --- | --- |
| **NOTES** | adds or corrects useful information in the relevant folder or repo context |
| **FOLDER** | new folder for work that will go on, with drafted context; moves the session in |
| **SKILL** | new folder for a way of working you'll want again |
| **LINK** | uses a markdown file an agent wrote (like a plan) as a folder's context |
| **RULE** | adds a rule you stated to your global `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md` |

Up to five cards are visible. Additional proposals stay in a saved backlog and receive a fresh Sol review before appearing. A session can produce several cards, including cards for other folders. Children inherit repo and parent context, so proposals add what is missing at the right scope.

Cards show the proposed change, destination, future usefulness, source and expandable evidence. Questions, answers, drafts and scroll positions survive leaving the card and restarting DEN. New answers stay marked unread until <kbd>ctrl+j</kbd> opens the latest conversation. Use <kbd>ctrl+d</kbd> to change destination, <kbd>ctrl+r</kbd> to request a revision, <kbd>ctrl+e</kbd> for evidence and <kbd>pgup/pgdn</kbd> to scroll. Moving or revising a proposal still needs acceptance before anything is applied.

Rejection requires a reason; an extra explanation is optional except for “Something else.” Feedback is recorded with the proposal and its model calls. A rejection applies to that information and destination, allowing later findings from the same session. Sol may also retain a general lesson in `~/.den/lessons.md`. See [card behavior and implementation](docs/card-review.md).

</details>

<details>
<summary><b>🔎 Where every session stands</b> · <kbd>z</kbd> for the wide view</summary>
<br>

<img src="docs/images/session-preview.png" alt="den's wide view with a session's goal, done and next." width="760">

Goal, done and next, the folder chain the session started with, your last prompt, the agent's last reply and the files it edited. <kbd>↵</kbd> on a closed session resumes it. Sessions you start outside den (an IDE, another terminal) show up in **Inbox**.

</details>

<details>
<summary><b>🪟 Agents side by side</b> · up to four, with status on every border</summary>
<br>

<img src="docs/images/agents-side-by-side.png" alt="den's column and two agents; agent 1 needs permission (amber border), agent 2 is working." width="760">

Each pane's border shows its key, status, folder and title. den names sessions with an emoji for their kind: 🐛 fix · ✨ build · 🔍 investigate · 📋 plan · 📝 docs · 🧹 clean up · 💬 question. A title you set with <kbd>r</kbd> stays.

</details>

<details>
<summary><b>🔒 Trust, privacy & cost</b></summary>
<br>

- **DEN's model never edits your code or executes workflows.** Background reviews have no tools; chat can read repository files when asked. DEN itself uses Git and tmux to manage agent sessions and worktrees.
- **Writes only with a yes:** a context file you linked, your global `CLAUDE.md`/`AGENTS.md` (RULE cards), your terminal's Option key (first run). Everything else lives in `~/.den`.
- **Leaves your Claude and Codex settings alone.** Context and a status hook go on each agent's command line.
- **Sends session excerpts and relevant context** to the model behind `codex` (OpenAI) or `claude` (Anthropic), for titles, reviews and chat. The local classifier runs offline; it does not replace those model calls. Local logs retain prompts, responses and feedback, which may include code.
- **Cost:** about 0.2¢ per progress review, about 4¢ per card review, chat answer or context change. The whole demo above came to under 30¢.

</details>

<details>
<summary><b>💡 Good to know</b></summary>
<br>

- **Status:** early · macOS only.
- **Agents share one checkout:** they run where you started den. To give agents their own checkout, run den from a git worktree. All of a repo's worktrees share one tree.
- **Your tmux setup is untouched:** den runs in its own tmux session, and its <kbd>⌥</kbd> keys only act in den's window.
- **Old sessions:** den drops a session from the Archive 25 days after archiving it, before Claude Code deletes transcripts at 30 days. What it found stays in its folder.

</details>

<details>
<summary><b>🗂️ Files</b></summary>
<br>

```
~/.den/repos/<repo>/den.json               folders, sessions, cards, discussions, feedback
~/.den/repos/<repo>/folders/*/context.md   each folder's context (repo/ is the repo's)
~/.den/repos/<repo>/*.jsonl                every suggestion, answer and model call
~/.den/lessons.md                          what den learned from your noes
```

`den eval export` packs these with the transcripts of tracked sessions, to send to the maintainer. It includes your prompts and code.

</details>

<details>
<summary><b>🛠️ Troubleshooting</b></summary>
<br>

- **⌥ keys type characters** like `¬`. **Terminal.app:** Settings → Profiles → Keyboard → *Use Option as Meta key*. **iTerm2:** Settings → Profiles → Keys → *Left Option key: Esc+*. **Ghostty:** `macos-option-as-alt = true`, then reload with <kbd>⌘</kbd><kbd>⇧</kbd><kbd>,</kbd>.
- **Sessions don't get named or chat cannot answer:** check the selected agent's login, model access and network connection. Codex takes precedence when both CLIs are on `PATH`. After 3 consecutive failed calls DEN pauses its model until the panel restarts; queued cards remain saved.
- **Local classifier unavailable:** missing runtime files allow direct model reviews. If you installed the runtime, check that `DEN_HOME` matches the installer and use [the classifier setup guide](docs/classifier-gate.md#install-or-restore). This message does not mean your sessions or context were lost.
- **An upgrade seems to do nothing:** check `command -v den` for a source/Homebrew conflict, then restart the old panel after your agents finish.
- **Uninstall:** `brew uninstall den`, or remove the source-installed `~/.local/bin/den`. Your notes and saved cards remain in `~/.den` (or `DEN_HOME`). Delete that directory only if you also want to permanently remove DEN's context, feedback and classifier runtime. Agent transcript storage is separate.

</details>

<br>
<p align="center"><sub>The screenshots and demo come from a small demo repo, with agents on Claude Haiku. 🛖</sub></p>
