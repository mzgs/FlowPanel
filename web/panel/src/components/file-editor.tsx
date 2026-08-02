import { useEffect, useRef } from "react";
import { indentWithTab } from "@codemirror/commands";
import { HighlightStyle, LanguageDescription, syntaxHighlighting } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { tags } from "@lezer/highlight";
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
      backgroundColor: "#151618",
      color: "#e6e6e8",
      fontSize: "13px",
    },
    "&.cm-focused": { outline: "none" },
    ".cm-scroller": {
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      lineHeight: "20px",
      overflow: "auto",
      scrollbarColor: "#393b42 transparent",
    },
    ".cm-content": { padding: "12px 0", caretColor: "#c4a7e7" },
    ".cm-line": { padding: "0 12px" },
    ".cm-gutters": {
      backgroundColor: "#121315",
      borderRight: "1px solid rgba(255, 255, 255, 0.065)",
      color: "#62646b",
    },
    ".cm-gutterElement": { padding: "0 12px 0 8px" },
    ".cm-activeLine": { backgroundColor: "rgba(255, 255, 255, 0.032)" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(196, 167, 231, 0.075)", color: "#aaa0b7" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
      backgroundColor: "rgba(130, 170, 255, 0.24)",
    },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#c4a7e7" },
    ".cm-matchingBracket": {
      backgroundColor: "rgba(196, 167, 231, 0.16)",
      color: "#e7d9f7",
      outline: "1px solid rgba(196, 167, 231, 0.38)",
    },
    ".cm-nonmatchingBracket": { backgroundColor: "rgba(239, 112, 112, 0.16)", color: "#ef7070" },
    ".cm-searchMatch": { backgroundColor: "rgba(230, 182, 115, 0.22)", outline: "none" },
    ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "rgba(230, 182, 115, 0.4)" },
    ".cm-selectionMatch": { backgroundColor: "rgba(130, 170, 255, 0.13)" },
    ".cm-foldPlaceholder": {
      backgroundColor: "#242529",
      border: "1px solid rgba(255, 255, 255, 0.08)",
      color: "#8c8e96",
    },
    ".cm-panels": { backgroundColor: "#121315", color: "#d8d8dc" },
    ".cm-panels.cm-panels-top": { borderBottom: "1px solid rgba(255, 255, 255, 0.07)" },
    ".cm-panel.cm-search": { padding: "6px 8px" },
    ".cm-textfield": {
      backgroundColor: "#1d1e21",
      border: "1px solid rgba(255, 255, 255, 0.1)",
      borderRadius: "6px",
      color: "#e6e6e8",
      outline: "none",
    },
    ".cm-button": {
      backgroundImage: "none",
      backgroundColor: "#28292e",
      border: "1px solid rgba(255, 255, 255, 0.09)",
      borderRadius: "6px",
      color: "#e6e6e8",
    },
    ".cm-tooltip": {
      backgroundColor: "#1d1e21",
      border: "1px solid rgba(255, 255, 255, 0.09)",
      borderRadius: "8px",
      color: "#e6e6e8",
      overflow: "hidden",
    },
    ".cm-tooltip-autocomplete > ul > li[aria-selected]": { backgroundColor: "rgba(196, 167, 231, 0.14)" },
  },
  { dark: true },
);

const editorHighlightStyle = HighlightStyle.define([
  { tag: [tags.meta, tags.comment], color: "#767980", fontStyle: "italic" },
  { tag: [tags.keyword, tags.modifier, tags.operatorKeyword], color: "#c4a7e7" },
  { tag: [tags.name, tags.variableName], color: "#e6e6e8" },
  { tag: [tags.definitionKeyword, tags.typeName, tags.className, tags.namespace], color: "#82aaff" },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: "#91b4ff" },
  { tag: [tags.propertyName, tags.attributeName, tags.labelName], color: "#d6b7f1" },
  { tag: [tags.string, tags.special(tags.string), tags.regexp], color: "#a8cc8c" },
  { tag: [tags.number, tags.bool, tags.null], color: "#e6b673" },
  { tag: [tags.operator, tags.punctuation, tags.separator], color: "#a5a7ad" },
  { tag: [tags.heading, tags.strong], color: "#e9d5ff", fontWeight: "600" },
  { tag: tags.emphasis, color: "#e9d5ff", fontStyle: "italic" },
  { tag: tags.link, color: "#82aaff", textDecoration: "underline" },
  { tag: [tags.invalid, tags.deleted], color: "#ef7070" },
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
          syntaxHighlighting(editorHighlightStyle),
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
    <div className="min-h-0 flex-1 overflow-hidden rounded-[10px] border border-white/[0.08] bg-[#151618] shadow-[0_10px_30px_rgba(0,0,0,0.18)]">
      <div className="flex h-8 items-center justify-between border-b border-white/[0.07] bg-[#121315] px-3 text-[11px] text-[#85878e]">
        <span className="flex items-center gap-2 font-medium text-[#aaa0b7]">
          <span className="size-1.5 rounded-full bg-[#c4a7e7] shadow-[0_0_8px_rgba(196,167,231,0.45)]" />
          {getLanguageLabel(path)}
        </span>
        <span className="flex items-center gap-1.5">
          <span className={`size-1.5 rounded-full ${readOnly ? "bg-[#767980]" : "bg-[#8fbc8f]"}`} />
          {readOnly ? "Read only" : "Editable"}
        </span>
      </div>
      <div ref={containerRef} className="h-[calc(100%-2rem)] min-h-[360px]" />
    </div>
  );
}
