import argparse
import hashlib
import json
import os
import pathlib
import resource
import statistics
import sys
import time

import psutil

from finetune_gliclass import metrics, operating_point, partition


DIRECTORY = pathlib.Path(".local/system-one-evaluation")
MODEL_DIRECTORY = pathlib.Path(".local/system-one-models")
QUESTIONS_PATH = pathlib.Path("experiments/instructions/standalone_gate_questions.json")
QUESTION = (
    "Does the NEW EXCHANGE contain information that warrants review under this saved instruction: "
    "{instruction}? Apply the instruction's own criteria. PREVIOUS CONTEXT only clarifies references; "
    "old information alone does not count."
)
RELATION_QUESTION = (
    "Is the NEW EXCHANGE related to this saved instruction: {instruction}? "
    "Base the answer on the NEW EXCHANGE; PREVIOUS CONTEXT only clarifies references."
)


def resident_bytes(processes):
    return sum(process.memory_info().rss for process in processes)


def render_states(case, tokenizer, windowed):
    new_exchange = "User: " + case["prompt"] + "\nAssistant: " + case["reply"]
    previous = case.get("previous")
    previous_context = ""
    if previous:
        previous_text = "User: " + previous["prompt"] + "\nAssistant: " + previous["reply"]
        previous_tokens = tokenizer.encode(previous_text, add_special_tokens=False)[-64:]
        previous_context = "\n\nPREVIOUS CONTEXT (reference only):\n" + tokenizer.decode(previous_tokens, skip_special_tokens=True)
    if not windowed:
        return ["NEW EXCHANGE:\n" + new_exchange + previous_context]
    exchange_tokens = tokenizer.encode(new_exchange, add_special_tokens=False)
    states = []
    for position in range(0, max(1, len(exchange_tokens)), 192):
        passage = tokenizer.decode(exchange_tokens[position:position + 256], skip_special_tokens=True)
        states.append("NEW EXCHANGE:\n" + passage + previous_context)
        if position + 256 >= len(exchange_tokens):
            break
    return states


def load_model(name):
    from transformers import AutoTokenizer

    if name.startswith("jev"):
        model_directory = MODEL_DIRECTORY / "jev-style-0.8b"
        sys.path.insert(0, str(model_directory.resolve()))
        import jev_style_decision_gguf

        jev_style_decision_gguf.LLAMA_N_CTX = 4096
        model = jev_style_decision_gguf.JevStyleDecisionGGUF(
            model_dir=model_directory,
            quant="Q4_K_M",
            binary=model_directory / "build/jev-score-cpu",
            max_len=3400,
            n_gpu_layers=0,
            threads=4,
        )
        tokenizer = AutoTokenizer.from_pretrained(model_directory / "tokenizer")
        processes = [psutil.Process(os.getpid()), psutil.Process(model.proc.pid)]
    else:
        from decider.infer import Decider

        model_directory = MODEL_DIRECTORY / "decider-2b"
        context_tokens = 1024 if name in {"decider-compact", "decider-relation", "decider-questions"} else 4096
        model = Decider(
            str(model_directory),
            gguf_file="decider-2b-v12-Q4_K_M.gguf",
            gguf_options={"n_gpu_layers": -1, "n_ctx": context_tokens, "n_threads": 4},
        )
        tokenizer = AutoTokenizer.from_pretrained(model_directory)
        processes = [psutil.Process(os.getpid())]
    return model, tokenizer, processes


def score_case(name, model, states, questions):
    if name.startswith("jev"):
        requests = [{"t": "noul", "ins": question, "crit": None} for question in questions]
        answers = model.decide_many(states[0], requests)
        return [answer["probabilities"]["true"] for answer in answers]
    scores = [0.0] * len(questions)
    for state in states:
        for position, question in enumerate(questions):
            maximum_context_tokens = 768 if name in {"decider-compact", "decider-relation", "decider-questions"} else 3000
            decision = model.decide(state, [{"question": question, "options": ["no", "yes"]}],
                                    max_ctx_tokens=maximum_context_tokens)[0]
            scores[position] = max(scores[position], decision["probs"]["yes"])
    return scores


def score(name, split):
    destination = DIRECTORY / name / (split + ".json")
    specification = partition(split)
    instructions = {instruction["identifier"]: instruction for instruction in specification["instructions"]}
    question_template = RELATION_QUESTION if name.endswith("relation") else QUESTION
    if name == "decider-questions":
        question_template = json.loads(QUESTIONS_PATH.read_text())
        if not set(instructions) <= set(question_template):
            raise ValueError("An instruction has no standalone question")
    if split != "development" and not (DIRECTORY / name / "selection.json").exists():
        raise SystemExit("Select the development threshold before scoring comparison cases")
    if split != "development":
        selection = json.loads((DIRECTORY / name / "selection.json").read_text())
        if selection["question"] != question_template:
            raise ValueError("Questions changed after threshold selection")
    if destination.exists():
        artifact = json.loads(destination.read_text())
        if artifact["data_sha256"] != hashlib.sha256(pathlib.Path(".local/gliclass-finetuning", split + ".json").read_bytes()).hexdigest():
            raise ValueError("Dataset changed after scoring began")
        if artifact["question"] != question_template:
            raise ValueError("Questions changed after scoring began")
        results = artifact["results"]
    else:
        results = []
    started = time.perf_counter()
    model, tokenizer, processes = load_model(name)
    load_seconds = time.perf_counter() - started
    peak_resident_bytes = resident_bytes(processes)
    if name.startswith("decider"):
        peak_resident_bytes = max(peak_resident_bytes, resource.getrusage(resource.RUSAGE_SELF).ru_maxrss)
    try:
        for case in specification["cases"][len(results):]:
            states = render_states(case, tokenizer, name in {"decider-windowed", "decider-compact", "decider-relation", "decider-questions"})
            identifiers = list(case["reviews"])
            if name == "decider-questions":
                questions = [question_template[identifier] for identifier in identifiers]
            else:
                questions = [question_template.format(instruction=instructions[identifier]["text"]) for identifier in identifiers]
            started = time.perf_counter()
            values = score_case(name, model, states, questions)
            wall_seconds = time.perf_counter() - started
            peak_resident_bytes = max(peak_resident_bytes, resident_bytes(processes))
            if name.startswith("decider"):
                peak_resident_bytes = max(peak_resident_bytes, resource.getrusage(resource.RUSAGE_SELF).ru_maxrss)
            results.append({
                "identifier": case["identifier"],
                "group": case["group"],
                "scores": dict(zip(identifiers, values)),
                "wall_seconds": wall_seconds,
            })
            if len(results) % 16 == 0 or len(results) == len(specification["cases"]):
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_text(json.dumps({
                    "model": name,
                    "device": "metal" if name.startswith("decider") else "cpu",
                    "split": split,
                    "data_sha256": hashlib.sha256(pathlib.Path(".local/gliclass-finetuning", split + ".json").read_bytes()).hexdigest(),
                    "question": question_template,
                    "script_sha256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),
                    "load_seconds": load_seconds,
                    "peak_resident_bytes": peak_resident_bytes,
                    "results": results,
                }, indent=2) + "\n")
                print(json.dumps({"model": name, "split": split, "completed": len(results), "total": len(specification["cases"])}), flush=True)
    finally:
        if name.startswith("jev"):
            model.close()


def select(name):
    specification = partition("development")
    actual = {case["identifier"]: case["reviews"] for case in specification["cases"]}
    artifact = json.loads((DIRECTORY / name / "development.json").read_text())
    if len(artifact["results"]) != len(actual):
        raise ValueError("Development scoring is incomplete")
    point = operating_point(artifact["results"], actual, 0.95)
    selection = {"model": name, "threshold": point["threshold"], "development": point["metrics"],
                 "question": artifact["question"], "development_sha256": hashlib.sha256((DIRECTORY / name / "development.json").read_bytes()).hexdigest()}
    destination = DIRECTORY / name / "selection.json"
    if destination.exists():
        raise SystemExit("Threshold already frozen")
    destination.write_text(json.dumps(selection, indent=2) + "\n")
    print(json.dumps({"threshold": point["threshold"], "reviews_avoided": point["metrics"]["reviews_avoided"],
                      "misses": point["metrics"]["misses"]}))


def report(name):
    selection = json.loads((DIRECTORY / name / "selection.json").read_text())
    for split in ["development", "historical", "heldout"]:
        specification = partition(split)
        actual = {case["identifier"]: case["reviews"] for case in specification["cases"]}
        artifact = json.loads((DIRECTORY / name / (split + ".json")).read_text())
        results = artifact["results"]
        if len(results) != len(actual):
            raise ValueError("Scoring is incomplete for " + split)
        measured = metrics(results, actual, selection["threshold"])
        wall_seconds = [result["wall_seconds"] for result in results]
        print(json.dumps({"model": name, "split": split, "threshold": selection["threshold"],
                          "reviews_avoided": measured["reviews_avoided"], "misses": measured["misses"],
                          "positives": measured["positives"], "unnecessary_reviews": measured["unnecessary_reviews"],
                          "median_case_seconds": statistics.median(wall_seconds),
                          "peak_resident_bytes": artifact["peak_resident_bytes"]}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["score", "select", "report"])
    parser.add_argument("--model", required=True, choices=["jev-style", "decider", "decider-windowed", "decider-compact",
                                                           "jev-relation", "decider-relation", "decider-questions"])
    parser.add_argument("--split", choices=["development", "historical", "heldout"], default="development")
    arguments = parser.parse_args()
    if arguments.action == "score":
        score(arguments.model, arguments.split)
    elif arguments.action == "select":
        select(arguments.model)
    else:
        report(arguments.model)
