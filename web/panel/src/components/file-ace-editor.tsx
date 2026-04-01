import AceEditor from "react-ace";
import "ace-builds/src-noconflict/mode-css";
import "ace-builds/src-noconflict/mode-golang";
import "ace-builds/src-noconflict/mode-html";
import "ace-builds/src-noconflict/mode-ini";
import "ace-builds/src-noconflict/mode-javascript";
import "ace-builds/src-noconflict/mode-json";
import "ace-builds/src-noconflict/mode-jsx";
import "ace-builds/src-noconflict/mode-markdown";
import "ace-builds/src-noconflict/mode-php";
import "ace-builds/src-noconflict/mode-python";
import "ace-builds/src-noconflict/mode-ruby";
import "ace-builds/src-noconflict/mode-sh";
import "ace-builds/src-noconflict/mode-sql";
import "ace-builds/src-noconflict/mode-text";
import "ace-builds/src-noconflict/mode-toml";
import "ace-builds/src-noconflict/mode-typescript";
import "ace-builds/src-noconflict/mode-xml";
import "ace-builds/src-noconflict/mode-yaml";
import "ace-builds/src-noconflict/theme-one_dark";

const aceModes = new Map<string, string>([
  ["bash", "sh"],
  ["conf", "text"],
  ["css", "css"],
  ["env", "sh"],
  ["go", "golang"],
  ["htm", "html"],
  ["html", "html"],
  ["ini", "ini"],
  ["js", "javascript"],
  ["json", "json"],
  ["jsx", "jsx"],
  ["log", "text"],
  ["md", "markdown"],
  ["php", "php"],
  ["py", "python"],
  ["rb", "ruby"],
  ["sh", "sh"],
  ["sql", "sql"],
  ["svg", "xml"],
  ["toml", "toml"],
  ["ts", "typescript"],
  ["tsx", "typescript"],
  ["txt", "text"],
  ["xml", "xml"],
  ["yaml", "yaml"],
  ["yml", "yaml"],
  ["zsh", "sh"],
]);

function getAceMode(path: string) {
  const fileName = path.split("/").pop()?.toLowerCase() ?? "";
  const dotIndex = fileName.lastIndexOf(".");
  const extension =
    dotIndex > 0 ? fileName.slice(dotIndex + 1) : fileName.startsWith(".") ? fileName.slice(1) : "";

  return aceModes.get(extension) || "text";
}

type FileAceEditorProps = {
  path: string;
  value: string;
  readOnly: boolean;
  onChange: (value: string) => void;
};

export function FileAceEditor({ path, value, readOnly, onChange }: FileAceEditorProps) {
  return (
    <div className="min-h-0 flex-1 overflow-hidden rounded-[10px] border border-[var(--app-border)] bg-[#1b1f27] [&_.ace_gutter]:bg-[#1b1f27] [&_.ace_gutter]:text-[#6f7682] [&_.ace_gutter-active-line]:bg-[#222733] [&_.ace_gutter-cell]:bg-transparent [&_.ace_print-margin]:bg-[#1b1f27] [&_.ace_scroller]:bg-[#1b1f27]">
      <AceEditor
        mode={getAceMode(path)}
        theme="one_dark"
        name={path || "flowpanel-file-editor"}
        width="100%"
        height="100%"
        value={value}
        onChange={onChange}
        readOnly={readOnly}
        style={{ backgroundColor: "#1b1f27" }}
        fontSize={13}
        showPrintMargin={false}
        showGutter
        highlightActiveLine={!readOnly}
        wrapEnabled
        editorProps={{ $blockScrolling: true }}
        setOptions={{
          useWorker: false,
          tabSize: 2,
          useSoftTabs: true,
          wrap: true,
          scrollPastEnd: 0.15,
          showLineNumbers: true,
        }}
      />
    </div>
  );
}
