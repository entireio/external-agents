import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execFile } from "node:child_process";

export default function (pi: ExtensionAPI) {
  function fireHook(hookName: string, data: Record<string, unknown>): Promise<void> {
    return new Promise((resolve) => {
      try {
        const child = execFile(
          "entire",
          ["hooks", "pi", hookName],
          {
            timeout: 10000,
            windowsHide: true,
          },
          () => resolve(),
        );
        child.stdin?.end(JSON.stringify(data));
      } catch {
        // best effort — don't block the agent
        resolve();
      }
    });
  }

  pi.on("tool_call", async (event) => {
    if (event.toolName !== "bash") {
      return;
    }

    const input = event.input as { command?: string };
    if (typeof input.command !== "string" || input.command.includes("GIT_TERMINAL_PROMPT=")) {
      return;
    }

    // Pi tool subprocesses may inherit a real TTY even though the agent cannot
    // answer hook prompts. Disable git/Entire terminal prompts for bash calls so
    // Entire treats agent-driven commits as non-interactive.
    input.command = "export GIT_TERMINAL_PROMPT=0\n" + input.command;
  });

  pi.on("session_start", async (_event, ctx) => {
    await fireHook("session_start", {
      type: "session_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("before_agent_start", async (event, ctx) => {
    await fireHook("before_agent_start", {
      type: "before_agent_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      prompt: event.prompt,
    });
  });

  pi.on("agent_end", async (_event, ctx) => {
    await fireHook("agent_end", {
      type: "agent_end",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("session_shutdown", async () => {
    await fireHook("session_shutdown", {
      type: "session_shutdown",
    });
  });
}
