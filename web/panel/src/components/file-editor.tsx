import { useMemo, useRef, type KeyboardEvent } from "react";

const languageLabels = new Map<string, string>([
  ["bash", "Shell"],
  ["conf", "Config"],
  ["css", "CSS"],
  ["env", "Env"],
  ["go", "Go"],
  ["htm", "HTML"],
  ["html", "HTML"],
  ["ini", "INI"],
  ["js", "JavaScript"],
  ["json", "JSON"],
  ["jsx", "JSX"],
  ["log", "Log"],
  ["md", "Markdown"],
  ["php", "PHP"],
  ["py", "Python"],
  ["rb", "Ruby"],
  ["sh", "Shell"],
  ["sql", "SQL"],
  ["svg", "SVG"],
  ["toml", "TOML"],
  ["ts", "TypeScript"],
  ["tsx", "TSX"],
  ["txt", "Text"],
  ["xml", "XML"],
  ["yaml", "YAML"],
  ["yml", "YAML"],
  ["zsh", "Shell"],
]);

function getExtension(path: string) {
  const fileName = path.split("/").pop()?.toLowerCase() ?? "";
  const dotIndex = fileName.lastIndexOf(".");
  return dotIndex > 0
    ? fileName.slice(dotIndex + 1)
    : fileName.startsWith(".")
      ? fileName.slice(1)
      : "";
}

function getLanguageLabel(path: string) {
  return languageLabels.get(getExtension(path)) || "Text";
}

type FileEditorProps = {
  path: string;
  value: string;
  readOnly: boolean;
  onChange: (value: string) => void;
};

export function FileEditor({ path, value, readOnly, onChange }: FileEditorProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const lineNumbersRef = useRef<HTMLPreElement | null>(null);
  const lineNumbers = useMemo(() => {
    const count = Math.max(1, value.split("\n").length);
    return Array.from({ length: count }, (_, index) => index + 1).join("\n");
  }, [value]);

  function syncScroll() {
    if (lineNumbersRef.current && textareaRef.current) {
      lineNumbersRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  }

  function updateSelection(start: number, end = start) {
    requestAnimationFrame(() => {
      textareaRef.current?.setSelectionRange(start, end);
    });
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== "Tab" || readOnly) {
      return;
    }

    event.preventDefault();

    const target = event.currentTarget;
    const { selectionStart, selectionEnd } = target;
    const indent = "  ";

    if (!event.shiftKey) {
      onChange(`${value.slice(0, selectionStart)}${indent}${value.slice(selectionEnd)}`);
      updateSelection(selectionStart + indent.length);
      return;
    }

    const lineStart = value.lastIndexOf("\n", selectionStart - 1) + 1;
    if (value.slice(lineStart, lineStart + indent.length) !== indent) {
      return;
    }

    onChange(`${value.slice(0, lineStart)}${value.slice(lineStart + indent.length)}`);
    updateSelection(
      Math.max(lineStart, selectionStart - indent.length),
      Math.max(lineStart, selectionEnd - indent.length),
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-hidden rounded-[10px] border border-[var(--app-border)] bg-[#1b1f27]">
      <div className="flex h-8 items-center justify-between border-b border-white/10 px-3 text-[11px] text-[#8c94a3]">
        <span>{getLanguageLabel(path)}</span>
        <span>{readOnly ? "Read only" : "Editable"}</span>
      </div>
      <div className="grid h-[calc(100%-2rem)] min-h-[360px] grid-cols-[52px_minmax(0,1fr)]">
        <pre
          ref={lineNumbersRef}
          aria-hidden="true"
          className="select-none overflow-hidden border-r border-white/10 bg-[#181c24] px-3 py-3 text-right font-mono text-[13px] leading-5 text-[#6f7682]"
        >
          {lineNumbers}
        </pre>
        <textarea
          ref={textareaRef}
          value={value}
          readOnly={readOnly}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          onScroll={syncScroll}
          className="h-full min-h-0 resize-none overflow-auto bg-[#1b1f27] px-3 py-3 font-mono text-[13px] leading-5 text-[#d9dee7] caret-white outline-none placeholder:text-[#6f7682] disabled:cursor-not-allowed disabled:opacity-70"
        />
      </div>
    </div>
  );
}
