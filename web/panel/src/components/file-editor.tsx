import { useEffect, useRef } from "react";
import { indentWithTab } from "@codemirror/commands";
import { LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { basicSetup } from "codemirror";

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

const editorTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "#1b1f27",
      color: "#d9dee7",
      fontSize: "13px",
    },
    ".cm-scroller": {
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      lineHeight: "20px",
      overflow: "auto",
    },
    ".cm-content": { padding: "12px 0", caretColor: "#ffffff" },
    ".cm-line": { padding: "0 12px" },
    ".cm-gutters": {
      backgroundColor: "#181c24",
      borderRight: "1px solid rgba(255, 255, 255, 0.1)",
      color: "#6f7682",
    },
    ".cm-gutterElement": { padding: "0 12px 0 8px" },
    ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "rgba(255, 255, 255, 0.035)" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
      backgroundColor: "rgba(80, 135, 225, 0.32)",
    },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#ffffff" },
    ".cm-panels": { backgroundColor: "#181c24", color: "#d9dee7" },
    ".cm-textfield": {
      backgroundColor: "#222731",
      border: "1px solid rgba(255, 255, 255, 0.12)",
      color: "#d9dee7",
    },
    ".cm-button": {
      backgroundImage: "none",
      backgroundColor: "#2b313d",
      border: "1px solid rgba(255, 255, 255, 0.12)",
      color: "#d9dee7",
    },
    ".cm-tooltip": {
      backgroundColor: "#222731",
      border: "1px solid rgba(255, 255, 255, 0.12)",
      color: "#d9dee7",
    },
  },
  { dark: true },
);

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
  const containerRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const valueRef = useRef(value);
  const onChangeRef = useRef(onChange);
  const languageCompartmentRef = useRef(new Compartment());
  const readOnlyCompartmentRef = useRef(new Compartment());

  onChangeRef.current = onChange;

  useEffect(() => {
    if (!containerRef.current) return;

    const readOnlyCompartment = readOnlyCompartmentRef.current;
    const view = new EditorView({
      parent: containerRef.current,
      state: EditorState.create({
        doc: valueRef.current,
        extensions: [
          basicSetup,
          keymap.of([indentWithTab]),
          EditorState.tabSize.of(2),
          editorTheme,
          languageCompartmentRef.current.of([]),
          readOnlyCompartment.of([
            EditorState.readOnly.of(readOnly),
            EditorView.editable.of(!readOnly),
          ]),
          EditorView.contentAttributes.of({ "aria-label": "File contents" }),
          EditorView.updateListener.of((update) => {
            if (!update.docChanged) return;
            const nextValue = update.state.doc.toString();
            if (nextValue === valueRef.current) return;
            valueRef.current = nextValue;
            onChangeRef.current(nextValue);
          }),
        ],
      }),
    });

    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, []);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || value === valueRef.current) return;
    valueRef.current = value;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
  }, [value]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      effects: readOnlyCompartmentRef.current.reconfigure([
        EditorState.readOnly.of(readOnly),
        EditorView.editable.of(!readOnly),
      ]),
    });
  }, [readOnly]);

  useEffect(() => {
    let cancelled = false;
    const description = LanguageDescription.matchFilename(languages, path);

    void (description?.load() ?? Promise.resolve(null)).then((support) => {
      const view = viewRef.current;
      if (cancelled || !view) return;
      view.dispatch({
        effects: languageCompartmentRef.current.reconfigure(support ?? []),
      });
    });

    return () => {
      cancelled = true;
    };
  }, [path]);

  return (
    <div className="min-h-0 flex-1 overflow-hidden rounded-[10px] border border-[var(--app-border)] bg-[#1b1f27]">
      <div className="flex h-8 items-center justify-between border-b border-white/10 px-3 text-[11px] text-[#8c94a3]">
        <span>{getLanguageLabel(path)}</span>
        <span>{readOnly ? "Read only" : "Editable"}</span>
      </div>
      <div ref={containerRef} className="h-[calc(100%-2rem)] min-h-[360px]" />
    </div>
  );
}
