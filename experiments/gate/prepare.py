import argparse
import collections
import hashlib
import json
import pathlib
import re


def read_records(path):
    if not path.exists():
        return []
    records = []
    for line in path.read_text().splitlines():
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return records


def fingerprint(text):
    return hashlib.sha256(text.encode()).hexdigest()


def typed_prompt(text):
    text = text.strip()
    if "## My request for Codex:" in text:
        text = text.split("## My request for Codex:", 1)[1].strip()
    if not text or text.startswith(("<", "# AGENTS.md", "[Request interrupted", "This session is being continued", "You are performing a CONTEXT")):
        return ""
    return text


def content_text(content):
    if isinstance(content, str):
        return content
    return "\n".join(block.get("text", "") for block in content if block.get("type") in ("text", "input_text", "output_text"))


def read_session(path, tool):
    records = read_records(path)
    metadata = next((record["payload"] for record in records if record.get("type") == "session_meta"), {})
    if isinstance(metadata.get("source"), dict) or metadata.get("source") == "exec":
        return None
    working_directory = metadata.get("cwd", "")
    session_identifier = metadata.get("id", path.stem)
    exchanges = []
    current = None
    for record in records:
        if record.get("isSidechain"):
            continue
        working_directory = record.get("cwd") or working_directory
        if tool == "claude":
            role = record.get("type")
            content = record.get("message", {}).get("content", [])
            if record.get("isMeta"):
                continue
        else:
            payload = record.get("payload", {})
            if record.get("type") != "response_item" or payload.get("type") != "message":
                if current and payload.get("type") in ("function_call", "custom_tool_call"):
                    changed_paths = re.findall(r"\*\*\* (?:Add|Update) File: (\S+)", payload.get("input", "") + payload.get("arguments", ""))
                    current["markdown_written"] |= any(is_user_markdown(name) for name in changed_paths)
                continue
            role = payload.get("role")
            content = payload.get("content", [])
        text = content_text(content)
        if role == "user" and typed_prompt(text):
            if current and current["reply"]:
                exchanges.append(current)
            current = {"prompt": typed_prompt(text), "reply": "", "at": record.get("timestamp", ""), "markdown_written": False}
        elif role == "assistant" and current:
            if text.strip():
                current["reply"] = text.strip()
            if tool == "claude" and isinstance(content, list):
                for block in content:
                    if block.get("type") == "tool_use" and block.get("name") in ("Write", "Edit", "MultiEdit"):
                        current["markdown_written"] |= is_user_markdown(block.get("input", {}).get("file_path", ""))
    if current and current["reply"]:
        exchanges.append(current)
    if not exchanges or "/.den/tmp" in working_directory or working_directory.startswith(("/private/tmp/", "/tmp/")):
        return None
    return {"identifier": session_identifier, "parent": metadata.get("forked_from_id"), "tool": tool,
            "working_directory": working_directory, "path": str(path), "exchanges": exchanges}


def is_user_markdown(name):
    path = pathlib.Path(name)
    return path.suffix.lower() == ".md" and not {"memory", "plans", ".den"}.intersection(path.parts)


def end_of_text(text, length):
    text = text.strip()
    if len(text) <= length:
        return text
    return "…" + text[-length + 1:]


def start_of_text(text, length):
    text = text.strip()
    if len(text) <= length:
        return text
    return text[:length - 1] + "…"


def local_verdict(exchange):
    if exchange["markdown_written"]:
        return "review"
    words = re.findall("[a-z0-9]+", exchange["prompt"].lower())
    go_ahead = set("yes y yeah yep ok okay sure go on ahead continue proceed do it next thanks thank you please and commit push".split())
    if words and set(words) <= go_ahead:
        return "skip"
    if exchange["reply"].strip().split("\n")[-1].rstrip(" *_`\"')").endswith("?"):
        return "skip"
    return "gate"


def gate_prompt(goal, exchange):
    return (f"SESSION\ntitle: (historical session)\nfolder: (historical assignment unavailable)\ngoal: {goal}\n"
            f"\nWAITING CARDS\n(none)\n\nNEW EXCHANGES\nexchange 1\nuser: {exchange['prompt']}\nagent: {exchange['reply']}\n")


def logged_gates(output_directory):
    examples = {}
    baseline = {}
    for path in (pathlib.Path.home() / ".den" / "repos").glob("*/calls.jsonl"):
        for record in read_records(path):
            answer = record.get("answer", {})
            if record.get("job") != "gate check" or record.get("error") or not isinstance(answer, dict) or not isinstance(answer.get("worthReview"), bool):
                continue
            identifier = fingerprint(record["prompt"])[:20]
            examples[identifier] = {"identifier": identifier, "text": record["prompt"], "split": "test", "session": record["sessionId"],
                                    "source_call": record["id"], "model": record["model"], "prompt_version": record["promptVersion"]}
            baseline[identifier] = dict(answer, identifier=identifier)
    directory = output_directory / "logged"
    directory.mkdir(exist_ok=True)
    (directory / "examples.jsonl").write_text("".join(json.dumps(record) + "\n" for record in examples.values()))
    (directory / "baseline.jsonl").write_text("".join(json.dumps(record) + "\n" for record in baseline.values()))


def prepare(output_directory, excluded_directory):
    sessions = []
    source_counts = collections.Counter()
    for directory, tool in [(".claude/projects", "claude"), (".codex/sessions", "codex"), (".codex/archived_sessions", "codex")]:
        for path in sorted((pathlib.Path.home() / directory).rglob("*.jsonl")):
            source_counts["files_seen"] += 1
            if "subagents" in path.parts:
                continue
            session = read_session(path, tool)
            if session and session["working_directory"] != excluded_directory:
                sessions.append(session)
    parents = {session["identifier"]: session["identifier"] for session in sessions}

    def root(identifier):
        parents.setdefault(identifier, identifier)
        while parents[identifier] != identifier:
            parents[identifier] = parents[parents[identifier]]
            identifier = parents[identifier]
        return identifier

    def merge(first, second):
        parents[root(first)] = root(second)

    fingerprints = {}
    first_prompts = {}
    for session in sessions:
        identifier = session["identifier"]
        if session["parent"]:
            merge(identifier, session["parent"])
        first_prompt = re.sub(r"\s+", " ", session["exchanges"][0]["prompt"]).lower()
        if len(first_prompt) > 120:
            first_prompt = first_prompt[:300]
            if first_prompt in first_prompts:
                merge(identifier, first_prompts[first_prompt])
            first_prompts[first_prompt] = identifier
        for exchange in session["exchanges"]:
            signature = fingerprint(re.sub(r"\s+", " ", exchange["prompt"] + "\n" + exchange["reply"]))
            if signature in fingerprints and len(exchange["prompt"] + exchange["reply"]) > 120:
                merge(identifier, fingerprints[signature])
            fingerprints[signature] = identifier
    groups = collections.defaultdict(list)
    for session in sessions:
        groups[root(session["identifier"])].append(session["identifier"])
    group_identifiers = {identifier: fingerprint("\n".join(sorted(members)))[:16]
                         for members in groups.values() for identifier in members}
    examples = []
    seen = set()
    for session in sessions:
        group = group_identifiers[session["identifier"]]
        split_number = int(fingerprint("den-gate-42:" + group)[:8], 16) / 2 ** 32
        split = "train"
        if split_number >= 0.85:
            split = "test"
        elif split_number >= 0.70:
            split = "validation"
        goal = start_of_text(session["exchanges"][0]["prompt"], 300)
        for index, exchange in enumerate(session["exchanges"]):
            clipped = dict(exchange, prompt=start_of_text(exchange["prompt"], 700), reply=end_of_text(exchange["reply"], 700))
            text = gate_prompt(goal, clipped)
            signature = fingerprint(text)
            if signature in seen:
                continue
            seen.add(signature)
            examples.append({"identifier": signature[:20], "session": session["identifier"], "group": group,
                             "split": split, "tool": session["tool"], "project": session["working_directory"],
                             "source": session["path"], "exchange": index + 1, "at": exchange["at"], "text": text,
                             "local_verdict": local_verdict(clipped)})
    output_directory.mkdir(parents=True, exist_ok=True)
    examples.sort(key=lambda example: example["identifier"])
    (output_directory / "examples.jsonl").write_text("".join(json.dumps(example, ensure_ascii=False) + "\n" for example in examples))
    summary = {"source_files": source_counts["files_seen"], "sessions": len(sessions), "groups": len(groups),
               "examples": len(examples), "splits": dict(collections.Counter(example["split"] for example in examples)),
               "tools": dict(collections.Counter(example["tool"] for example in examples)),
               "source_sha256": {session["path"]: fingerprint(pathlib.Path(session["path"]).read_text()) for session in sessions}}
    (output_directory / "manifest.json").write_text(json.dumps(summary, indent=2))
    logged_gates(output_directory)
    print(json.dumps({key: value for key, value in summary.items() if key != "source_sha256"}, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--exclude-directory", default=str(pathlib.Path.cwd()))
    arguments = parser.parse_args()
    prepare(arguments.output, arguments.exclude_directory)
