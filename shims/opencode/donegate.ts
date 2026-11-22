// donegate shim
import { spawnSync } from "node:child_process";

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

function pickData(event: any): Record<string, any> {
  if (!event || typeof event !== "object") return {};
  if (event.data && typeof event.data === "object") return event.data;
  if (event.properties && typeof event.properties === "object") return event.properties;
  return event;
}

function pickInfo(data: Record<string, any>): Record<string, any> {
  if (data.info && typeof data.info === "object") return data.info;
  if (data.session && typeof data.session === "object") return data.session;
  return {};
}

function pickSessionID(event: any): string | null {
  const data = pickData(event);
  const info = pickInfo(data);
  for (const id of [data.sessionID, info.id, info.sessionID]) {
    if (typeof id === "string" && id) return id;
  }
  return null;
}

function pickParentID(event: any): string | null {
  const data = pickData(event);
  const info = pickInfo(data);
  for (const id of [info.parentID, data.parentID]) {
    if (typeof id === "string" && id) return id;
  }
  return null;
}

function pickDirectory(event: any, fallback: string): string {
  const data = pickData(event);
  const info = pickInfo(data);
  for (const dir of [info.directory, data.directory, event?.location?.directory]) {
    if (typeof dir === "string" && dir) return dir;
  }
  return fallback;
}

function guardToolCall(rawToolName: unknown, rawArgs: unknown, cwd: string, sessionID?: unknown): void {
  const rawTool = String(rawToolName ?? "").toLowerCase();
  const args = (rawArgs && typeof rawArgs === "object" ? rawArgs : {}) as Record<string, unknown>;
  const isShell = rawTool === "bash" || rawTool === "shell";
  if (!isShell && !WRITE_TOOLS.has(rawTool)) return;

  const payload = isShell
    ? { tool: "bash", paths: [] as string[], command: typeof args.command === "string" ? args.command : "", cwd }
    : { tool: rawTool, paths: extractPaths(args), command: "", cwd };

  const session = typeof sessionID === "string" && sessionID ? sessionID : undefined;
  const res = runDonegate<GuardRes>(["guard", "--format", "json"], { ...payload, session }, 10_000);
  if (res && res.allow === false) throw new Error(res.reason || "donegate: blocked");
}

function createEventProcessor(
  defaultCwd: string,
  logMessage: (msg: string) => Promise<void>,
  promptSession: (sessionID: string, text: string) => Promise<void>,
) {
  const childSessions = new Set<string>();
  const gatedSessions = new Set<string>();
  const startedSessions = new Set<string>();
  let startedAnonymous = false;

  // Start the donegate session the first time we see a session. `opencode run` can create its
  // session before the event subscription is live, so session.created may never reach us; the
  // first guarded tool call always comes before any write, so the baseline is still correct.
  const ensureSession = (sessionID: unknown, cwd: string, source = "startup") => {
    const sid = typeof sessionID === "string" && sessionID ? sessionID : "";
    if (sid ? startedSessions.has(sid) : startedAnonymous) return;
    if (sid) startedSessions.add(sid);
    else startedAnonymous = true;
    runDonegate(["session", "start", "--format", "json", "--harness", "opencode"], { cwd, session: sid, source }, 30_000);
  };

  return {
    ensureSession,

    clearGated(sessionID: unknown) {
      if (typeof sessionID === "string" && sessionID) gatedSessions.delete(sessionID);
    },

    async handleEvent(event: any) {
      try {
        const type = typeof event?.type === "string" ? event.type
          : typeof event?.name === "string" ? event.name : null;
        if (!type) return;

        const sid = pickSessionID(event);
        const parentID = pickParentID(event);
        if (parentID) {
          if (sid) childSessions.add(sid);
          return;
        }
        if (sid && childSessions.has(sid)) return;

        const eventCwd = pickDirectory(event, defaultCwd);

        if (type === "session.created") {
          if (!sid) return;
          gatedSessions.delete(sid);
          ensureSession(sid, eventCwd);
        } else if (type === "session.compacted") {
          if (!sid) return;
          runDonegate(["session", "start", "--format", "json", "--harness", "opencode"], {
            cwd: eventCwd,
            session: sid,
            source: "compact",
          }, 30_000);
        } else if (type === "session.execution.started") {
          if (sid) gatedSessions.delete(sid);
        } else if (type === "session.status") {
          const statusType = pickData(event)?.status?.type;
          if (sid && (statusType === "busy" || statusType === "retry")) {
            gatedSessions.delete(sid);
          }
        } else if (type === "session.execution.succeeded" || type === "session.idle") {
          if (!sid || gatedSessions.has(sid)) return;
          gatedSessions.add(sid);
          ensureSession(sid, eventCwd);
          const res = runDonegate<GateRes>(["gate", "--format", "json"], { cwd: eventCwd, session: sid }, 600_000);
          if (!res) return;
          if (res.user_message) {
            try {
              await logMessage(res.user_message);
            } catch {
              console.error(res.user_message);
            }
          }
          if (res.block && res.message) {
            try {
              await promptSession(sid, res.message);
            } catch {
              // fail open
            }
          }
        }
      } catch {
        // fail open on internal errors
      }
    },
  };
}

const DonegatePlugin = {
  id: "donegate",

  async setup(ctx: any) {
    const cwd = ctx?.location?.directory || process.cwd();
    const controller = new AbortController();

    const processor = createEventProcessor(
      cwd,
      async (message) => {
        if (ctx?.client?.app?.log) {
          await ctx.client.app.log({ body: { service: "donegate", level: "info", message } });
        } else {
          console.error(message);
        }
      },
      async (sessionID, text) => {
        if (typeof ctx?.session?.prompt === "function") {
          void Promise.resolve(ctx.session.prompt({ sessionID, text })).catch(() => {});
        } else if (ctx?.client?.session?.prompt) {
          void Promise.resolve(
            ctx.client.session.prompt({
              path: { id: sessionID },
              body: { parts: [{ type: "text", text }] },
            }),
          ).catch(() => {});
        }
      },
    );

    try {
      await ctx?.tool?.hook?.("execute.before", (e: any) => {
        processor.ensureSession(e?.sessionID, cwd);
        processor.clearGated(e?.sessionID);
        guardToolCall(e?.tool, e?.input, cwd, e?.sessionID);
      });
    } catch {
      // fail open on internal hook registration errors
    }

    if (typeof ctx?.event?.subscribe === "function") {
      void (async () => {
        try {
          for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
            await processor.handleEvent(event);
          }
        } catch {
          // aborted or bus closed
        }
      })();
    }

    return () => {
      try {
        controller.abort();
      } catch {
        // fail open
      }
    };
  },

  async server({ client, directory, worktree }: any = {}) {
    const cwd = directory || worktree || process.cwd();
    const processor = createEventProcessor(
      cwd,
      async (message) => {
        if (client?.app?.log) {
          await client.app.log({ body: { service: "donegate", level: "info", message } });
        } else {
          console.error(message);
        }
      },
      async (sessionID, text) => {
        if (client?.session?.prompt) {
          await client.session.prompt({
            path: { id: sessionID },
            body: { parts: [{ type: "text", text }] },
          });
        }
      },
    );

    return {
      event: async ({ event }: any) => processor.handleEvent(event),
      "tool.execute.before": async (input: any, output: any) => {
        processor.ensureSession(input?.sessionID, cwd);
        processor.clearGated(input?.sessionID);
        guardToolCall(input?.tool, output?.args, cwd, input?.sessionID);
      },
    };
  },
};

export default DonegatePlugin;
export { DonegatePlugin };
