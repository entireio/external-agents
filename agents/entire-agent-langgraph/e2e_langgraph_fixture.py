from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import TypedDict

from entire_adapter import EntireCallbackHandler
from langchain_core.runnables import RunnableConfig
from langchain_core.tools import tool
from langgraph.graph import END, START, StateGraph


class State(TypedDict):
    prompt: str
    path: str
    content: str
    result: str


@tool
def write_file(path: str, content: str) -> str:
    """Write content to a repository file."""

    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")
    return f"wrote {path}"


def parse_prompt(prompt: str) -> tuple[str, str]:
    match = re.search(
        r"file called\s+([A-Za-z0-9_.-]+).*?(?:content|containing)\s+'([^']*)'",
        prompt,
        flags=re.IGNORECASE | re.DOTALL,
    )
    if match:
        return match.group(1), match.group(2) + "\n"

    fallback = re.search(r"file called\s+([A-Za-z0-9_.-]+)", prompt, flags=re.IGNORECASE)
    if fallback:
        path = fallback.group(1)
        return path, Path(path).stem + "\n"

    return "langgraph-output.txt", prompt + "\n"


def node(state: State, config: RunnableConfig) -> State:
    result = write_file.invoke(
        {"path": state["path"], "content": state["content"]},
        config=config,
    )
    return {**state, "result": str(result)}


def main() -> int:
    prompt = " ".join(sys.argv[1:]).strip()
    if not prompt:
        print("usage: e2e_langgraph_fixture.py <prompt>", file=sys.stderr)
        return 2

    path, content = parse_prompt(prompt)

    graph = StateGraph(State)
    graph.add_node("write", node)
    graph.add_edge(START, "write")
    graph.add_edge("write", END)

    handler = EntireCallbackHandler(
        agent_label="e2e-langgraph",
        repo_path=str(Path.cwd()),
        checkpoint_policy={"write_file": "always"},
    )
    result = graph.compile().invoke(
        {"prompt": prompt, "path": path, "content": content, "result": ""},
        config={"callbacks": [handler]},
    )

    tmp_dir = Path(".entire") / "tmp"
    tmp_dir.mkdir(parents=True, exist_ok=True)
    marker = tmp_dir / f"{handler.session_id}.json"
    marker.write_text(
        json.dumps(
            {
                "session_id": handler.session_id,
                "agent_name": "langgraph",
                "session_ref": handler.session_ref,
                "prompt": prompt,
                "result": result,
            },
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )

    print(f"LangGraph result: {result}")
    print(f"Entire session: {handler.session_id}")
    print(f"Entire session ref: {handler.session_ref}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
