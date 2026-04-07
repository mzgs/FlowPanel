import { useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { RotateCcw, Trash2 } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type ConnectionState = "connecting" | "connected" | "disconnected" | "error";

type TerminalServerMessage = {
  type: "ready" | "exit" | "error";
  cwd?: string;
  message?: string;
  pid?: number;
  shell?: string;
};

type SessionMeta = {
  cwd: string;
  pid: number | null;
  shell: string;
};

function buildTerminalSocketURL() {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  return `${protocol}://${window.location.host}/api/terminal/ws`;
}

function getStatusClassName(status: ConnectionState) {
  switch (status) {
    case "connected":
      return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "connecting":
      return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "error":
      return "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300";
    default:
      return "border-border bg-muted text-muted-foreground";
  }
}

function writeTerminalNotice(terminal: Terminal, text: string) {
  terminal.writeln("");
  terminal.writeln(text);
}

export function TerminalPage() {
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

  const [sessionKey, setSessionKey] = useState(0);
  const [connectionState, setConnectionState] = useState<ConnectionState>("connecting");
  const [statusText, setStatusText] = useState("Connecting to local shell");
  const [sessionMeta, setSessionMeta] = useState<SessionMeta>({
    cwd: "",
    pid: null,
    shell: "",
  });

  useEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return;
    }

    let disposed = false;

    const terminal = new Terminal({
      allowTransparency: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "block",
      fontFamily: 'SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.35,
      scrollback: 5000,
      theme: {
        background: "#1e1e1e",
        cursor: "#f5f5f5",
        cursorAccent: "#1e1e1e",
        foreground: "#f5f5f5",
        selectionBackground: "rgba(255,255,255,0.18)",
      },
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(host);
    fitAddon.fit();
    terminal.focus();
    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const sendMessage = (payload: object) => {
      const socket = socketRef.current;
      if (!socket || socket.readyState !== WebSocket.OPEN) {
        return;
      }

      socket.send(JSON.stringify(payload));
    };

    const syncSize = () => {
      const currentFitAddon = fitAddonRef.current;
      const currentTerminal = terminalRef.current;

      if (!currentFitAddon || !currentTerminal) {
        return;
      }

      currentFitAddon.fit();
      if (currentTerminal.cols > 0 && currentTerminal.rows > 0) {
        sendMessage({
          type: "resize",
          cols: currentTerminal.cols,
          rows: currentTerminal.rows,
        });
      }
    };

    const dataSubscription = terminal.onData((data) => {
      sendMessage({
        type: "input",
        data,
      });
    });

    const socket = new WebSocket(buildTerminalSocketURL());
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;

    const resizeObserver = new ResizeObserver(() => {
      syncSize();
    });
    resizeObserver.observe(host);

    setConnectionState("connecting");
    setStatusText("Connecting to local shell");
    setSessionMeta({
      cwd: "",
      pid: null,
      shell: "",
    });

    socket.onopen = () => {
      if (disposed) {
        return;
      }

      syncSize();
    };

    socket.onmessage = async (event) => {
      if (disposed) {
        return;
      }

      if (typeof event.data === "string") {
        let message: TerminalServerMessage;
        try {
          message = JSON.parse(event.data) as TerminalServerMessage;
        } catch {
          return;
        }

        switch (message.type) {
          case "ready":
            setConnectionState("connected");
            setStatusText("Connected");
            setSessionMeta({
              cwd: message.cwd ?? "",
              pid: message.pid ?? null,
              shell: message.shell ?? "",
            });
            syncSize();
            break;
          case "exit":
            setConnectionState("disconnected");
            setStatusText(message.message ?? "Shell exited");
            writeTerminalNotice(terminal, message.message ?? "Shell exited.");
            break;
          case "error":
            setConnectionState("error");
            setStatusText(message.message ?? "Terminal unavailable");
            writeTerminalNotice(terminal, message.message ?? "Terminal unavailable.");
            break;
        }

        return;
      }

      if (event.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(event.data));
      }
    };

    socket.onerror = () => {
      if (disposed) {
        return;
      }

      setConnectionState("error");
      setStatusText("Connection error");
    };

    socket.onclose = () => {
      if (disposed) {
        return;
      }

      setConnectionState((current) => (current === "error" ? current : "disconnected"));
      setStatusText((current) =>
        current === "Connected" || current === "Connecting to local shell" ? "Session closed" : current,
      );
    };

    const animationFrame = window.requestAnimationFrame(() => {
      syncSize();
    });

    return () => {
      disposed = true;
      window.cancelAnimationFrame(animationFrame);
      resizeObserver.disconnect();
      dataSubscription.dispose();
      socket.close();
      socketRef.current = null;
      fitAddonRef.current = null;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [sessionKey]);

  const actions = (
    <>
      <Button type="button" variant="outline" onClick={() => terminalRef.current?.clear()}>
        <Trash2 className="h-4 w-4" />
        Clear
      </Button>
      <Button type="button" variant="outline" onClick={() => setSessionKey((current) => current + 1)}>
        <RotateCcw className="h-4 w-4" />
        Reconnect
      </Button>
    </>
  );

  return (
    <>
      <PageHeader
        title="Terminal"
        meta="Interactive local shell running on the FlowPanel host."
        actions={actions}
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
          <div className="flex items-center justify-between border-b border-zinc-700 bg-zinc-800 px-4 py-3 text-sm text-zinc-300">
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
              <span className="h-3 w-3 rounded-full bg-[#febc2e]" />
              <span className="h-3 w-3 rounded-full bg-[#28c840]" />
            </div>
            <div className="truncate font-medium text-zinc-200">
              {sessionMeta.shell ? `${sessionMeta.shell} session` : "Local terminal"}
            </div>
            <div
              className={cn(
                "rounded-md border px-2.5 py-1 text-xs font-medium",
                getStatusClassName(connectionState),
              )}
            >
              {statusText}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b px-4 py-3 text-sm text-muted-foreground">
            <div>
              Host: <span className="font-mono text-foreground">local</span>
            </div>
            <div>
              Shell: <span className="font-mono text-foreground">{sessionMeta.shell || "waiting"}</span>
            </div>
            <div className="min-w-0">
              Directory:{" "}
              <span className="font-mono text-foreground" title={sessionMeta.cwd || undefined}>
                {sessionMeta.cwd || "waiting"}
              </span>
            </div>
            <div>
              PID: <span className="font-mono text-foreground">{sessionMeta.pid ?? "waiting"}</span>
            </div>
          </div>

          <div className="bg-[#1e1e1e] px-3 py-3 sm:px-4">
            <div
              ref={hostRef}
              className="h-[34rem] overflow-hidden rounded-md border border-zinc-800 bg-[#1e1e1e] sm:h-[40rem]"
              onClick={() => terminalRef.current?.focus()}
            />
          </div>
        </section>
      </div>
    </>
  );
}
