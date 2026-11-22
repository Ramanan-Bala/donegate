// donegate shim
import { spawnSync } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { isToolCallEventType } from "@earendil-works/pi-coding-agent";

const INSTALLED_BIN: string = "__DONEGATE_BIN__";

type GuardRes = { allow?: boolean; reason?: string };
type GateRes = { block?: boolean; message?: string; user_message?: string; verdict?: string };
type StartRes = { context?: string };

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

export default function (pi: ExtensionAPI) {
  let pendingContext: string | undefined;

  pi.on("session_start", (event, ctx) => {
    const source = event.reason === "resume" || event.reason === "reload" ? "resume" : "startup";
    const res = runDonegate<StartRes>(
      ["session", "start", "--format", "json", "--harness", "pi"],
      { cwd: ctx.cwd, session: ctx.sessionManager.getSessionId(), source },
      30_000,
    );
    if (res?.context) pendingContext = res.context;
  });

  pi.on("before_agent_start", () => {
    if (!pendingContext) return;
    const content = pendingContext;
    pendingContext = undefined;
    return { message: { customType: "donegate", content, display: false } };
  });

  pi.on("tool_call", (event, ctx) => {
    let payload: { tool: string; paths: string[]; command: string; cwd: string } | null = null;
    if (isToolCallEventType("bash", event)) {
      payload = { tool: "bash", paths: [], command: event.input.command ?? "", cwd: ctx.cwd };
    } else if (isToolCallEventType("powershell", event)) {
      payload = { tool: "bash", paths: [], command: event.input.command ?? "", cwd: ctx.cwd };
    } else if (isToolCallEventType("edit", event)) {
      payload = { tool: "edit", paths: event.input.path ? [event.input.path] : [], command: "", cwd: ctx.cwd };
    } else if (isToolCallEventType("write", event)) {
      payload = { tool: "write", paths: event.input.path ? [event.input.path] : [], command: "", cwd: ctx.cwd };
    }
    if (!payload) return;

    const res = runDonegate<GuardRes>(
      ["guard", "--format", "json"],
      { ...payload, session: ctx.sessionManager.getSessionId() },
      10_000,
    );
    if (res && res.allow === false) {
      return { block: true, reason: res.reason || "donegate: blocked" };
    }
  });

  pi.on("agent_before_settle", (event, ctx) => {
    if (event.outcome !== "completed") return;
    const res = runDonegate<GateRes>(
      ["gate", "--format", "json"],
      { cwd: ctx.cwd, session: ctx.sessionManager.getSessionId() },
      600_000,
    );
    if (!res) return;
    if (res.user_message) {
      try {
        ctx.ui.notify(res.user_message, res.verdict === "FAIL" ? "warning" : "info");
      } catch {
        // fail open
      }
    }
    if (res.block && res.message) {
      return {
        entries: [
          ...event.entries,
          { type: "custom_message", customType: "donegate", content: res.message, display: true },
        ],
        continue: true,
      };
    }
  });
}
