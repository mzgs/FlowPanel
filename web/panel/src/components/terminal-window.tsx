import { useEffect, useRef, useState, type ReactNode } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { Trash2 } from "@/components/icons/tabler-icons";
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

type TerminalWindowProps = {
  cwd?: string;
  cwdLabel?: string;
  title?: ReactNode;
  className?: string;
  heightClassName?: string;
};

function buildTerminalSocketURL(cwd?: string) {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  const url = new URL(`${protocol}://${window.location.host}/api/terminal/ws`);
  const normalizedCwd = cwd?.trim();

  if (normalizedCwd) {
    url.searchParams.set("cwd", normalizedCwd);
  }

  return url.toString();
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

export function TerminalWindow({
  cwd,
  cwdLabel,
  title,
  className,
  heightClassName = "h-[26rem] sm:h-[32rem]",
}: TerminalWindowProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

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
        background: "#000000",
        cursor: "#f5f5f5",
        cursorAccent: "#000000",
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

    const socket = new WebSocket(buildTerminalSocketURL(cwd));
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
  }, [cwd]);

  const displayTitle = title ?? (sessionMeta.shell ? `${sessionMeta.shell} session` : "Local terminal");
  const displayPath = sessionMeta.cwd || cwdLabel;

  return (
    <section
      className={cn(
        "overflow-hidden rounded-xl border border-border bg-card shadow-sm",
        className,
      )}
    >
      <div className="flex items-center justify-between gap-3 border-b border-zinc-800 bg-[#101010] px-4 py-3 text-sm text-zinc-300">
        <div className="flex items-center gap-2">
          <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
          <span className="h-3 w-3 rounded-full bg-[#febc2e]" />
          <span className="h-3 w-3 rounded-full bg-[#28c840]" />
        </div>
        <div className="min-w-0 flex-1 truncate text-center font-medium text-zinc-200">
          {displayTitle}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-zinc-700 bg-zinc-900 text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            aria-label="Clear terminal"
            title="Clear terminal"
            onClick={() => terminalRef.current?.clear()}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
          <div
            className={cn(
              "rounded-md border px-2.5 py-1 text-xs font-medium",
              getStatusClassName(connectionState),
            )}
          >
            {statusText}
          </div>
        </div>
      </div>

      <div className="bg-black px-3 py-3 sm:px-4">
        <div
          ref={hostRef}
          className={cn("overflow-hidden rounded-md bg-black", heightClassName)}
          aria-label={displayPath}
          onClick={() => terminalRef.current?.focus()}
        />
      </div>
    </section>
  );
}
