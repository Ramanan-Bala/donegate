// donegate shim
import { spawnSync } from "node:child_process";
import type { Plugin } from "@opencode-ai/plugin";

const INSTALLED_BIN: string = "__DONEGATE_BIN__";
const WRITE_TOOLS = new Set(["edit", "write", "patch", "apply_patch", "multiedit", "notebookedit"]);

type GuardRes = { allow?: boolean; reason?: string };
type GateRes = { block?: boolean; message?: string; user_message?: string };

function resolveBin(): string {
  if (process.env.DONEGATE_BIN) return process.env.DONEGATE_BIN;
  if (INSTALLED_BIN && INSTALLED_BIN !== "__DONEGATE_" + "BIN__") return INSTALLED_BIN;
  return "donegate";
}

function runDonegate<T>(args: string[], payload: unknown, timeout: number): T | null {
  try {
    const res = spawnSync(resolveBin(), args, {
      input: JSON.stringify(payload),
      encoding: "utf8",
      timeout,
      windowsHide: true,
    });
    if (res.error || !res.stdout) return null;
    return JSON.parse(res.stdout.trim()) as T;
  } catch {
    return null;
  }
}

function extractPaths(args: Record<string, unknown>): string[] {
  const paths: string[] = [];
  for (const key of ["filePath", "path", "file"]) {
    if (typeof args[key] === "string" && args[key]) paths.push(args[key]);
  }
  for (const key of ["files", "edits"]) {
    const list = args[key];
    if (!Array.isArray(list)) continue;
    for (const item of list) {
      if (typeof item === "string" && item) paths.push(item);
      else if (item && typeof item === "object") {
        const rec = item as Record<string, unknown>;
        const p = rec.filePath ?? rec.path ?? rec.file;
        if (typeof p === "string" && p) paths.push(p);
      }
    }
  }
  if (typeof args.patchText === "string") {
    for (const m of args.patchText.matchAll(/^\*\*\* (?:Add|Update|Delete|Move to) File: (.+)$/gm)) {
      if (m[1]) paths.push(m[1].trim());
    }
  }
  return paths;
}

export const DonegatePlugin: Plugin = async ({ client, directory, worktree }) => {
  const cwd = directory || worktree || process.cwd();

  return {
    event: async ({ event }) => {
      try {
        if (event.type === "session.created") {
          const info = event.properties.info;
          runDonegate(["session", "start", "--format", "json", "--harness", "opencode"], {
            cwd: info.directory || cwd,
            session: info.id,
            source: "startup",
          }, 30_000);
        } else if (event.type === "session.compacted") {
          runDonegate(["session", "start", "--format", "json", "--harness", "opencode"], {
            cwd,
            session: event.properties.sessionID,
            source: "compact",
          }, 30_000);
        } else if (event.type === "session.idle") {
          const sessionID = event.properties.sessionID;
          const res = runDonegate<GateRes>(["gate", "--format", "json"], { cwd, session: sessionID }, 600_000);
          if (!res) return;
          if (res.user_message) {
            try {
              await client.app.log({ body: { service: "donegate", level: "info", message: res.user_message } });
            } catch {
              console.error(res.user_message);
            }
          }
          if (res.block && res.message) {
            await client.session.prompt({
              path: { id: sessionID },
              body: { parts: [{ type: "text", text: res.message }] },
            });
          }
        }
      } catch {
        // fail open on internal errors
      }
    },

    "tool.execute.before": async (input, output) => {
      const rawTool = (input.tool ?? "").toLowerCase();
      const args = (output?.args && typeof output.args === "object" ? output.args : {}) as Record<string, unknown>;
      const isShell = rawTool === "bash" || rawTool === "shell";
      if (!isShell && !WRITE_TOOLS.has(rawTool)) return;

      const payload = isShell
        ? { tool: "bash", paths: [] as string[], command: typeof args.command === "string" ? args.command : "", cwd }
        : { tool: rawTool, paths: extractPaths(args), command: "", cwd };

      const res = runDonegate<GuardRes>(["guard", "--format", "json"], payload, 10_000);
      if (res && res.allow === false) throw new Error(res.reason || "donegate: blocked");
    },
  };
};

export default DonegatePlugin;
