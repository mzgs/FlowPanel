import {
  Suspense,
  lazy,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUp,
  Check,
  Copy,
  Download,
  File,
  FileCode2,
  FilePlus2,
  FileSymlink,
  Folder,
  FolderOpen,
  FolderPlus,
  Grid2X2,
  HardDrive,
  List,
  Pencil,
  RefreshCw,
  Scissors,
  Search,
  Settings2,
  Trash2,
  Upload,
} from "@/components/icons/tabler-icons";
import {
  createDirectory,
  createFile,
  deleteEntry,
  fetchFileContent,
  fetchFiles,
  getDownloadUrl,
  renameEntry,
  saveFileContent,
  transferEntries,
  uploadFiles,
  type FileEntry,
  type FileListing,
} from "@/api/files";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { formatBytes, formatDateTime } from "@/lib/format";
import { consumePendingFilesPath } from "@/lib/files-navigation";
import { cn } from "@/lib/utils";

type ViewMode = "list" | "grid";
type DialogMode = "folder" | "file" | "rename" | null;
type ClipboardMode = "copy" | "move" | null;
type FlashTone = "success" | "error";

type FlashMessage = {
  tone: FlashTone;
  text: string;
};

type ContextMenuState = {
  x: number;
  y: number;
  scope: "item" | "background";
  path: string | null;
};

type MarqueeState = {
  active: boolean;
  startX: number;
  startY: number;
  currentX: number;
  currentY: number;
  hasMoved: boolean;
  baseSelection: string[];
};

type SettingsState = {
  startPath: string;
  preferredView: ViewMode;
};

const VIEW_STORAGE_KEY = "flowpanel.files.view";
const START_PATH_STORAGE_KEY = "flowpanel.files.start-path";
const LAST_PATH_STORAGE_KEY = "flowpanel.files.last-path";

const editableExtensions = new Set([
  "bash",
  "conf",
  "css",
  "env",
  "go",
  "htm",
  "html",
  "ini",
  "js",
  "json",
  "jsx",
  "log",
  "md",
  "php",
  "py",
  "rb",
  "sh",
  "sql",
  "svg",
  "toml",
  "ts",
  "tsx",
  "txt",
  "xml",
  "yaml",
  "yml",
  "zsh",
]);

const FileAceEditor = lazy(() =>
  import("@/components/file-ace-editor").then((module) => ({ default: module.FileAceEditor })),
);

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function readStoredViewMode(): ViewMode {
  if (typeof window === "undefined") {
    return "list";
  }

  return window.localStorage.getItem(VIEW_STORAGE_KEY) === "grid" ? "grid" : "list";
}

function readStoredStartPath() {
  if (typeof window === "undefined") {
    return "";
  }

  return window.localStorage.getItem(START_PATH_STORAGE_KEY) || "";
}

function readStoredLastPath() {
  if (typeof window === "undefined") {
    return null;
  }

  return window.localStorage.getItem(LAST_PATH_STORAGE_KEY);
}

function isEditableFile(item: FileEntry) {
  if (item.type !== "file") {
    return false;
  }

  if (!item.extension) {
    return true;
  }

  return editableExtensions.has(item.extension);
}

function getItemLabel(item: FileEntry) {
  if (item.type === "directory") {
    return "Folder";
  }

  if (item.type === "symlink") {
    return "Symlink";
  }

  return item.extension ? `${item.extension.toUpperCase()} file` : "File";
}

function getItemIcon(item: FileEntry) {
  if (item.type === "directory") {
    return Folder;
  }

  if (item.type === "symlink") {
    return FileSymlink;
  }

  if (isEditableFile(item)) {
    return FileCode2;
  }

  return File;
}

function getGridPreviewTone(item: FileEntry) {
  if (item.type === "directory") {
    return {
      frame: "border-emerald-900/70 bg-emerald-950/80 text-emerald-300",
      glow: "shadow-[inset_0_1px_0_rgba(255,255,255,0.03),0_10px_24px_rgba(16,185,129,0.18)]",
    };
  }

  if (item.type === "symlink") {
    return {
      frame: "border-sky-900/70 bg-sky-950/80 text-sky-300",
      glow: "shadow-[inset_0_1px_0_rgba(255,255,255,0.03),0_10px_24px_rgba(14,165,233,0.18)]",
    };
  }

  if (isEditableFile(item)) {
    return {
      frame: "border-amber-900/70 bg-amber-950/80 text-amber-300",
      glow: "shadow-[inset_0_1px_0_rgba(255,255,255,0.03),0_10px_24px_rgba(245,158,11,0.18)]",
    };
  }

  return {
    frame: "border-slate-700 bg-slate-900/80 text-slate-300",
    glow: "shadow-[inset_0_1px_0_rgba(255,255,255,0.03),0_10px_24px_rgba(148,163,184,0.16)]",
  };
}

function getBreadcrumbs(listing: FileListing | undefined) {
  const rootName = listing?.root_name || "Sites";
  const breadcrumbs = [{ label: rootName, path: "" }];

  if (!listing?.path) {
    return breadcrumbs;
  }

  const segments = listing.path.split("/").filter(Boolean);
  let cursor = "";
  for (const segment of segments) {
    cursor = cursor ? `${cursor}/${segment}` : segment;
    breadcrumbs.push({ label: segment, path: cursor });
  }

  return breadcrumbs;
}

function pathLabel(path: string) {
  return path || "/";
}

function FlashBanner({ flash }: { flash: FlashMessage }) {
  return (
    <div
      className={cn(
        "rounded-[12px] border px-4 py-3 text-[13px]",
        flash.tone === "success"
          ? "border-[var(--app-ok)]/30 bg-[var(--app-ok-soft)] text-[var(--app-text)]"
          : "border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] text-[var(--app-text)]",
      )}
    >
      {flash.text}
    </div>
  );
}

function EmptyState({ searchActive }: { searchActive: boolean }) {
  return (
    <div className="flex min-h-[260px] flex-col items-center justify-center gap-3 px-6 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-full border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
        {searchActive ? <Search className="h-5 w-5" /> : <FolderOpen className="h-5 w-5" />}
      </div>
      <div>
        <div className="text-[15px] font-medium text-[var(--app-text)]">
          {searchActive ? "No items match this filter." : "This folder is empty."}
        </div>
        <p className="mt-1 max-w-sm text-[13px] leading-6 text-[var(--app-text-muted)]">
          {searchActive
            ? "Try a different search term or clear the filter."
            : "Create a folder, add a file, or drop uploads here to populate this location."}
        </p>
      </div>
    </div>
  );
}

export function FilesPage() {
  const queryClient = useQueryClient();
  const browserRef = useRef<HTMLDivElement | null>(null);
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const contextMenuRef = useRef<HTMLDivElement | null>(null);

  const [currentPath, setCurrentPath] = useState(() => {
    const pendingPath = consumePendingFilesPath();
    if (pendingPath !== null) {
      return pendingPath;
    }

    const lastPath = readStoredLastPath();
    return lastPath ?? readStoredStartPath();
  });
  const [selectedPaths, setSelectedPaths] = useState<string[]>([]);
  const [anchorPath, setAnchorPath] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useState<ViewMode>(readStoredViewMode);
  const [dialogMode, setDialogMode] = useState<DialogMode>(null);
  const [dialogValue, setDialogValue] = useState("");
  const [confirmDeletePaths, setConfirmDeletePaths] = useState<string[]>([]);
  const [flash, setFlash] = useState<FlashMessage | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [clipboardMode, setClipboardMode] = useState<ClipboardMode>(null);
  const [clipboardPaths, setClipboardPaths] = useState<string[]>([]);
  const [dropTargetPath, setDropTargetPath] = useState<string | null>(null);
  const [rootDropActive, setRootDropActive] = useState(false);
  const [marquee, setMarquee] = useState<MarqueeState>({
    active: false,
    startX: 0,
    startY: 0,
    currentX: 0,
    currentY: 0,
    hasMoved: false,
    baseSelection: [],
  });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settings, setSettings] = useState<SettingsState>({
    startPath: readStoredStartPath(),
    preferredView: readStoredViewMode(),
  });
  const [editorOpen, setEditorOpen] = useState(false);
  const [editorPath, setEditorPath] = useState("");
  const [editorName, setEditorName] = useState("");
  const [editorContent, setEditorContent] = useState("");
  const [editorOriginalContent, setEditorOriginalContent] = useState("");
  const [editorMeta, setEditorMeta] = useState<{ size: number; modifiedAt: string } | null>(null);
  const [editorBusy, setEditorBusy] = useState(false);

  const listingQuery = useQuery({
    queryKey: ["files", currentPath],
    queryFn: () => fetchFiles(currentPath),
  });

  const listing = listingQuery.data;
  const allItems = listing ? [...listing.directories, ...listing.files] : [];
  const itemOrder = allItems.map((item) => item.path);
  const itemMap = new Map(allItems.map((item) => [item.path, item] as const));
  const normalizedSearch = search.trim().toLowerCase();
  const filteredItems = allItems.filter((item) => {
    if (!normalizedSearch) {
      return true;
    }

    return (
      item.name.toLowerCase().includes(normalizedSearch) ||
      item.path.toLowerCase().includes(normalizedSearch)
    );
  });
  const selectedSet = new Set(selectedPaths);
  const selectedItems = selectedPaths
    .map((path) => itemMap.get(path) ?? null)
    .filter((item): item is FileEntry => item !== null);
  const selectedItem = selectedItems.length === 1 ? selectedItems[0] : null;
  const confirmDeleteSinglePath = confirmDeletePaths.length === 1 ? confirmDeletePaths[0] : null;
  const confirmDeleteSingleName = confirmDeleteSinglePath
    ? itemMap.get(confirmDeleteSinglePath)?.name ?? confirmDeleteSinglePath
    : null;
  const breadcrumbs = getBreadcrumbs(listing);
  const clipboardReady = clipboardPaths.length > 0 && clipboardMode !== null;
  const summaryParts = [
    `${listing?.directories.length ?? 0} folders`,
    `${listing?.files.length ?? 0} files`,
  ];

  if (selectedPaths.length > 0) {
    summaryParts.push(`${selectedPaths.length} selected`);
  }
  if (clipboardReady) {
    summaryParts.push(`${clipboardMode === "copy" ? "Copy" : "Cut"} ready: ${clipboardPaths.length}`);
  }

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    window.localStorage.setItem(VIEW_STORAGE_KEY, viewMode);
  }, [viewMode]);

  useEffect(() => {
    if (typeof window === "undefined" || !listingQuery.isSuccess) {
      return;
    }

    window.localStorage.setItem(LAST_PATH_STORAGE_KEY, listing?.path ?? currentPath);
  }, [currentPath, listing?.path, listingQuery.isSuccess]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    if (settings.startPath) {
      window.localStorage.setItem(START_PATH_STORAGE_KEY, settings.startPath);
    } else {
      window.localStorage.removeItem(START_PATH_STORAGE_KEY);
    }
  }, [settings.startPath]);

  useEffect(() => {
    const available = new Set(itemOrder);
    const nextSelection = selectedPaths.filter((path) => available.has(path));
    if (nextSelection.length !== selectedPaths.length) {
      setSelectedPaths(nextSelection);
      if (anchorPath && !available.has(anchorPath)) {
        setAnchorPath(nextSelection.at(-1) ?? null);
      }
    }
  }, [anchorPath, itemOrder, selectedPaths]);

  useEffect(() => {
    if (!listingQuery.isError || !currentPath) {
      return;
    }

    if (getErrorMessage(listingQuery.error, "").toLowerCase().includes("not found")) {
      const storedLastPath = readStoredLastPath();
      const storedStartPath = readStoredStartPath();
      const fromLastPath = storedLastPath === currentPath;
      const fromStartPath = storedStartPath === currentPath;

      if (typeof window !== "undefined" && fromLastPath) {
        window.localStorage.removeItem(LAST_PATH_STORAGE_KEY);
      }

      setCurrentPath("");
      setSearch("");
      setContextMenu(null);
      clearSelection();

      if (fromStartPath) {
        setSettings((current) => ({ ...current, startPath: "" }));
      }

      if (fromLastPath && storedStartPath && storedStartPath !== currentPath) {
        setCurrentPath(storedStartPath);
        return;
      }

      if (fromLastPath && !fromStartPath) {
        return;
      }

      if (fromStartPath) {
        return;
      }
    }
  }, [currentPath, listingQuery.error, listingQuery.isError]);

  useEffect(() => {
    function handleDocumentClick(event: MouseEvent) {
      if (!contextMenuRef.current || !contextMenu) {
        return;
      }

      if (!contextMenuRef.current.contains(event.target as Node)) {
        setContextMenu(null);
      }
    }

    function handleEscape(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable='true']")) {
        if (event.key === "Escape") {
          setContextMenu(null);
        }
        return;
      }

      const meta = event.metaKey || event.ctrlKey;

      if (event.key === "Escape") {
        if (marquee.active) {
          setMarquee((current) => ({ ...current, active: false, hasMoved: false }));
          return;
        }
        if (contextMenu) {
          setContextMenu(null);
          return;
        }
        setSelectedPaths([]);
        setAnchorPath(null);
        return;
      }

      if (meta && event.key.toLowerCase() === "a") {
        if (itemOrder.length > 0) {
          setSelectedPaths(itemOrder);
          setAnchorPath(itemOrder[0] ?? null);
          event.preventDefault();
        }
        return;
      }

      if (meta && event.key.toLowerCase() === "c" && selectedPaths.length > 0) {
        setClipboardMode("copy");
        setClipboardPaths(selectedPaths);
        setFlash({ tone: "success", text: `Copied ${selectedPaths.length} item(s) to the panel clipboard.` });
        event.preventDefault();
        return;
      }

      if (meta && event.key.toLowerCase() === "x" && selectedPaths.length > 0) {
        setClipboardMode("move");
        setClipboardPaths(selectedPaths);
        setFlash({ tone: "success", text: `Cut ${selectedPaths.length} item(s).` });
        event.preventDefault();
        return;
      }

      if (meta && event.key.toLowerCase() === "v" && clipboardReady) {
        void pasteInto(currentPath);
        event.preventDefault();
        return;
      }

      if (event.key === "Delete" && selectedPaths.length > 0) {
        handleDeleteSelection();
        event.preventDefault();
      }
    }

    document.addEventListener("click", handleDocumentClick);
    document.addEventListener("keydown", handleEscape);

    return () => {
      document.removeEventListener("click", handleDocumentClick);
      document.removeEventListener("keydown", handleEscape);
    };
  }, [clipboardReady, contextMenu, currentPath, itemOrder, marquee.active, selectedPaths]);

  useEffect(() => {
    if (!marquee.active) {
      return;
    }

    function handleMouseMove(event: MouseEvent) {
      setMarquee((current) => {
        const width = Math.abs(event.clientX - current.startX);
        const height = Math.abs(event.clientY - current.startY);
        return {
          ...current,
          currentX: event.clientX,
          currentY: event.clientY,
          hasMoved: current.hasMoved || width > 4 || height > 4,
        };
      });
    }

    function handleMouseUp() {
      setMarquee((current) => ({
        ...current,
        active: false,
        hasMoved: false,
        baseSelection: [],
      }));
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);

    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [marquee.active]);

  useEffect(() => {
    if (!marquee.active || !marquee.hasMoved || !browserRef.current) {
      return;
    }

    const left = Math.min(marquee.startX, marquee.currentX);
    const right = Math.max(marquee.startX, marquee.currentX);
    const top = Math.min(marquee.startY, marquee.currentY);
    const bottom = Math.max(marquee.startY, marquee.currentY);
    const next = new Set(marquee.baseSelection);

    const candidates = browserRef.current.querySelectorAll<HTMLElement>("[data-selectable='1']");
    for (const element of candidates) {
      const rect = element.getBoundingClientRect();
      if (rect.right < left || rect.left > right || rect.bottom < top || rect.top > bottom) {
        continue;
      }
      const path = element.dataset.path || "";
      if (path) {
        next.add(path);
      }
    }

    const ordered = itemOrder.filter((path) => next.has(path));
    setSelectedPaths(ordered);
    if (ordered.length > 0) {
      setAnchorPath(ordered.at(-1) ?? null);
    }
  }, [itemOrder, marquee]);

  const createDirectoryMutation = useMutation({
    mutationFn: createDirectory,
    onSuccess: async () => {
      await invalidateCurrentListing();
      setDialogMode(null);
      setDialogValue("");
      setFlash({ tone: "success", text: "Folder created." });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to create folder.") });
    },
  });

  const createFileMutation = useMutation({
    mutationFn: createFile,
    onSuccess: async () => {
      await invalidateCurrentListing();
      setDialogMode(null);
      setDialogValue("");
      setFlash({ tone: "success", text: "File created." });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to create file.") });
    },
  });

  const renameMutation = useMutation({
    mutationFn: renameEntry,
    onSuccess: async (nextPath) => {
      await invalidateCurrentListing();
      setSelectedPaths([nextPath]);
      setAnchorPath(nextPath);
      setDialogMode(null);
      setDialogValue("");
      setFlash({ tone: "success", text: "Entry renamed." });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to rename entry.") });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (paths: string[]) => {
      for (const path of paths) {
        await deleteEntry(path);
      }
    },
    onSuccess: async () => {
      await invalidateCurrentListing();
      setSelectedPaths([]);
      setAnchorPath(null);
      setContextMenu(null);
      setFlash({ tone: "success", text: "Selection deleted." });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to delete selection.") });
    },
    onSettled: () => {
      setConfirmDeletePaths([]);
    },
  });

  const uploadMutation = useMutation({
    mutationFn: ({ path, files }: { path: string; files: File[] }) => uploadFiles(path, files),
    onSuccess: async () => {
      await invalidateCurrentListing();
      setDropTargetPath(null);
      setRootDropActive(false);
      setFlash({ tone: "success", text: "Upload complete." });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to upload files.") });
    },
  });

  const transferMutation = useMutation({
    mutationFn: transferEntries,
    onSuccess: async (_, variables) => {
      await invalidateCurrentListing();
      setDropTargetPath(null);
      setRootDropActive(false);
      setContextMenu(null);
      setSelectedPaths([]);
      setAnchorPath(null);
      if (variables.mode === "move") {
        setClipboardMode(null);
        setClipboardPaths([]);
      }
      setFlash({
        tone: "success",
        text: variables.mode === "copy" ? "Selection copied." : "Selection moved.",
      });
    },
    onError: (error) => {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to transfer selection.") });
    },
  });

  async function invalidateCurrentListing() {
    await queryClient.invalidateQueries({ queryKey: ["files", currentPath] });
  }

  function setSelection(paths: string[], nextAnchor?: string | null) {
    const deduped = itemOrder.filter((path, index) => {
      return paths.includes(path) && itemOrder.indexOf(path) === index;
    });
    setSelectedPaths(deduped);
    setAnchorPath(nextAnchor === undefined ? (deduped.at(-1) ?? null) : nextAnchor);
  }

  function clearSelection() {
    setSelectedPaths([]);
    setAnchorPath(null);
  }

  function handleNavigate(path: string) {
    setCurrentPath(path);
    setSearch("");
    setContextMenu(null);
    clearSelection();
  }

  function handleSelectionClick(item: FileEntry, event: ReactMouseEvent<HTMLElement>) {
    const withMeta = event.metaKey || event.ctrlKey;
    const withShift = event.shiftKey;

    if (!withMeta && !withShift && item.type === "directory") {
      handleNavigate(item.path);
      return;
    }

    if (withShift && anchorPath) {
      const start = itemOrder.indexOf(anchorPath);
      const end = itemOrder.indexOf(item.path);
      if (start >= 0 && end >= 0) {
        const range = itemOrder.slice(Math.min(start, end), Math.max(start, end) + 1);
        setSelection(range, anchorPath);
        return;
      }
    }

    if (withMeta) {
      if (selectedSet.has(item.path)) {
        const next = selectedPaths.filter((path) => path !== item.path);
        setSelection(next, next.at(-1) ?? null);
      } else {
        setSelection([...selectedPaths, item.path], item.path);
      }
      return;
    }

    setSelection([item.path], item.path);
  }

  function handleActivateItem(item: FileEntry) {
    if (item.type === "directory") {
      handleNavigate(item.path);
      return;
    }

    if (item.type === "symlink") {
      setFlash({ tone: "error", text: "Symlinks are not supported in the panel." });
      return;
    }

    void openEditor(item.path);
  }

  async function openEditor(path: string) {
    setEditorOpen(true);
    setEditorBusy(true);
    setEditorPath(path);
    setEditorName(path.split("/").pop() || path);
    setEditorContent("");
    setEditorOriginalContent("");
    setEditorMeta(null);

    try {
      const file = await fetchFileContent(path);
      setEditorName(file.name);
      setEditorContent(file.content);
      setEditorOriginalContent(file.content);
      setEditorMeta({ size: file.size, modifiedAt: file.modified_at });
    } catch (error) {
      setEditorOpen(false);
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to open file.") });
    } finally {
      setEditorBusy(false);
    }
  }

  async function saveEditor() {
    if (!editorPath) {
      return;
    }

    setEditorBusy(true);

    try {
      await saveFileContent({ path: editorPath, content: editorContent });
      setEditorOriginalContent(editorContent);
      await invalidateCurrentListing();
      setFlash({ tone: "success", text: "File saved." });
    } catch (error) {
      setFlash({ tone: "error", text: getErrorMessage(error, "Failed to save file.") });
    } finally {
      setEditorBusy(false);
    }
  }

  function openDialog(mode: Exclude<DialogMode, null>) {
    setDialogMode(mode);
    setDialogValue(mode === "rename" && selectedItem ? selectedItem.name : "");
    setContextMenu(null);
  }

  async function submitDialog(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const name = dialogValue.trim();
    if (!name) {
      return;
    }

    if (dialogMode === "folder") {
      await createDirectoryMutation.mutateAsync({ path: currentPath, name });
      return;
    }

    if (dialogMode === "file") {
      await createFileMutation.mutateAsync({ path: currentPath, name });
      return;
    }

    if (dialogMode === "rename" && selectedItem) {
      await renameMutation.mutateAsync({ path: selectedItem.path, name });
    }
  }

  function handleDeleteSelection() {
    if (selectedPaths.length === 0) {
      return;
    }

    if (deleteMutation.isPending) {
      return;
    }

    setConfirmDeletePaths(selectedPaths);
  }

  function confirmDeleteSelection() {
    if (confirmDeletePaths.length === 0 || deleteMutation.isPending) {
      return;
    }

    deleteMutation.mutate(confirmDeletePaths);
  }

  function copySelection(mode: Exclude<ClipboardMode, null>) {
    if (selectedPaths.length === 0) {
      return;
    }

    setClipboardMode(mode);
    setClipboardPaths(selectedPaths);
    setContextMenu(null);
    setFlash({
      tone: "success",
      text: mode === "copy" ? `Copied ${selectedPaths.length} item(s).` : `Cut ${selectedPaths.length} item(s).`,
    });
  }

  async function pasteInto(targetPath: string) {
    if (!clipboardReady) {
      return;
    }

    await transferMutation.mutateAsync({
      mode: clipboardMode === "copy" ? "copy" : "move",
      paths: clipboardPaths,
      target: targetPath,
    });
  }

  function handleDownloadSelection() {
    if (selectedItem?.type !== "file") {
      return;
    }

    window.location.assign(getDownloadUrl(selectedItem.path));
  }

  function handleUploadSelection(files: FileList | null, targetPath = currentPath) {
    if (!files || files.length === 0) {
      return;
    }

    uploadMutation.mutate({ path: targetPath, files: Array.from(files) });
  }

  function beginMarquee(event: ReactMouseEvent<HTMLDivElement>) {
    if (event.button !== 0) {
      return;
    }

    const target = event.target as HTMLElement;
    if (target.closest("[data-selectable='1']") || target.closest("[data-context-menu='1']")) {
      return;
    }

    setContextMenu(null);
    setMarquee({
      active: true,
      startX: event.clientX,
      startY: event.clientY,
      currentX: event.clientX,
      currentY: event.clientY,
      hasMoved: false,
      baseSelection: event.metaKey || event.ctrlKey ? selectedPaths : [],
    });

    if (!(event.metaKey || event.ctrlKey)) {
      clearSelection();
    }
  }

  function handleItemContextMenu(event: ReactMouseEvent<HTMLElement>, item: FileEntry) {
    event.preventDefault();

    if (!selectedSet.has(item.path)) {
      setSelection([item.path], item.path);
    }

    setContextMenu({
      x: event.clientX,
      y: event.clientY,
      scope: "item",
      path: item.path,
    });
  }

  function handleBackgroundContextMenu(event: ReactMouseEvent<HTMLDivElement>) {
    const target = event.target as HTMLElement;
    if (target.closest("[data-selectable='1']")) {
      return;
    }

    event.preventDefault();
    setContextMenu({
      x: event.clientX,
      y: event.clientY,
      scope: "background",
      path: null,
    });
  }

  function handleInternalDragStart(item: FileEntry, event: React.DragEvent<HTMLElement>) {
    if (!selectedSet.has(item.path)) {
      setSelection([item.path], item.path);
    }

    event.dataTransfer.setData("application/x-flowpanel-paths", JSON.stringify(selectedSet.has(item.path) ? selectedPaths : [item.path]));
    event.dataTransfer.effectAllowed = "move";
    setContextMenu(null);
  }

  function handleDirectoryDragOver(path: string, event: React.DragEvent<HTMLElement>) {
    const isFileDrop = event.dataTransfer.types.includes("Files");
    const hasInternalPaths = event.dataTransfer.types.includes("application/x-flowpanel-paths");

    if (!isFileDrop && !hasInternalPaths) {
      return;
    }

    event.preventDefault();
    setDropTargetPath(path);
    setRootDropActive(false);
  }

  function handleDirectoryDrop(path: string, event: React.DragEvent<HTMLElement>) {
    event.preventDefault();
    setDropTargetPath(null);

    if (event.dataTransfer.files.length > 0) {
      handleUploadSelection(event.dataTransfer.files, path);
      return;
    }

    const payload = event.dataTransfer.getData("application/x-flowpanel-paths");
    if (!payload) {
      return;
    }

    try {
      const paths = JSON.parse(payload) as string[];
      if (paths.length === 0) {
        return;
      }
      transferMutation.mutate({ mode: "move", paths, target: path });
    } catch {
      setFlash({ tone: "error", text: "Invalid drag payload." });
    }
  }

  function handleBrowserDragOver(event: React.DragEvent<HTMLDivElement>) {
    const target = event.target as HTMLElement;
    if (target.closest("[data-type='directory']")) {
      return;
    }

    if (event.dataTransfer.types.includes("Files")) {
      event.preventDefault();
      setRootDropActive(true);
      setDropTargetPath(null);
    }
  }

  function handleBrowserDrop(event: React.DragEvent<HTMLDivElement>) {
    const target = event.target as HTMLElement;
    if (target.closest("[data-type='directory']")) {
      return;
    }

    if (event.dataTransfer.files.length > 0) {
      event.preventDefault();
      setRootDropActive(false);
      handleUploadSelection(event.dataTransfer.files, currentPath);
    }
  }

  function buildContextMenuItems() {
    const targetItem = contextMenu?.path ? itemMap.get(contextMenu.path) ?? null : null;
    const items: Array<{
      key: string;
      label: string;
      icon: React.ComponentType<{ className?: string }>;
      disabled?: boolean;
      handler: () => void;
    }> = [];

    if (contextMenu?.scope === "item" && targetItem && selectedPaths.length === 1) {
      if (targetItem.type === "directory") {
        items.push({
          key: "open",
          label: "Open",
          icon: FolderOpen,
          handler: () => handleNavigate(targetItem.path),
        });
      }

      if (targetItem.type === "file") {
        items.push({
          key: "edit",
          label: isEditableFile(targetItem) ? "Open in Editor" : "Try Open",
          icon: Pencil,
          handler: () => void openEditor(targetItem.path),
        });
        items.push({
          key: "download",
          label: "Download",
          icon: Download,
          handler: handleDownloadSelection,
        });
      }
    }

    if (selectedPaths.length > 0) {
      items.push({
        key: "copy",
        label: selectedPaths.length === 1 ? "Copy" : `Copy ${selectedPaths.length} Items`,
        icon: Copy,
        handler: () => copySelection("copy"),
      });
      items.push({
        key: "cut",
        label: selectedPaths.length === 1 ? "Cut" : `Cut ${selectedPaths.length} Items`,
        icon: Scissors,
        handler: () => copySelection("move"),
      });

      if (selectedPaths.length === 1) {
        items.push({
          key: "rename",
          label: "Rename",
          icon: Pencil,
          handler: () => openDialog("rename"),
        });
      }

      items.push({
        key: "delete",
        label: selectedPaths.length === 1 ? "Delete" : `Delete ${selectedPaths.length} Items`,
        icon: Trash2,
        handler: handleDeleteSelection,
      });
    }

    if (clipboardReady) {
      const pasteTarget =
        contextMenu?.scope === "item" && targetItem?.type === "directory" ? targetItem.path : currentPath;
      items.push({
        key: "paste",
        label: clipboardMode === "copy" ? "Paste Copy" : "Paste Move",
        icon: Check,
        handler: () => {
          void pasteInto(pasteTarget);
        },
      });
    }

    return items;
  }

  const contextMenuItems = buildContextMenuItems();
  const isMutating =
    createDirectoryMutation.isPending ||
    createFileMutation.isPending ||
    renameMutation.isPending ||
    deleteMutation.isPending ||
    uploadMutation.isPending ||
    transferMutation.isPending;
  const dialogTitle =
    dialogMode === "folder" ? "New Folder" : dialogMode === "file" ? "New File" : "Rename Entry";
  const editorDirty = editorContent !== editorOriginalContent;
  const marqueeRect = marquee.hasMoved
    ? {
        left: Math.min(marquee.startX, marquee.currentX),
        top: Math.min(marquee.startY, marquee.currentY),
        width: Math.abs(marquee.currentX - marquee.startX),
        height: Math.abs(marquee.currentY - marquee.startY),
      }
    : null;
  const contextMenuStyle = contextMenu
    ? {
        left:
          typeof window === "undefined"
            ? contextMenu.x
            : Math.max(12, Math.min(contextMenu.x, window.innerWidth - 252)),
        top:
          typeof window === "undefined"
            ? contextMenu.y
            : Math.max(12, Math.min(contextMenu.y, window.innerHeight - 260)),
      }
    : null;

  return (
    <div className="min-h-[calc(100vh-var(--app-navbar-height))]">
      <ActionConfirmDialog
        open={confirmDeletePaths.length > 0}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) {
            setConfirmDeletePaths([]);
          }
        }}
        title={confirmDeletePaths.length > 1 ? "Delete selected items" : "Delete item"}
        desc={
          confirmDeletePaths.length > 1
            ? `Delete ${confirmDeletePaths.length} selected items?`
            : confirmDeleteSingleName
              ? `Delete "${confirmDeleteSingleName}"?`
              : "Delete this item?"
        }
        confirmText={confirmDeletePaths.length > 1 ? "Delete items" : "Delete item"}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={confirmDeleteSelection}
        className="sm:max-w-md"
      />
      <PageHeader
        title="Files"
        meta={
          listing
            ? `Root ${listing.root_path}`
            : "Browse, multi-select, drag, copy, move, and edit files in the panel."
        }
      />

      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <div className="space-y-4">
          {flash ? <FlashBanner flash={flash} /> : null}

          <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
            <div className="flex flex-col gap-4 border-b border-[var(--app-border)] px-4 py-4 md:px-5">
              <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-[12px] uppercase tracking-[0.2em] text-[var(--app-text-muted)]">
                    <HardDrive className="h-4 w-4" />
                    File root
                  </div>
                  <div className="text-[15px] font-medium text-[var(--app-text)]">
                    {listing?.absolute_path || listing?.root_path || "Loading..."}
                  </div>
                  <div className="text-[12px] text-[var(--app-text-muted)]">{summaryParts.join(" / ")}</div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => listingQuery.refetch()}
                    disabled={listingQuery.isFetching}
                  >
                    <RefreshCw className={cn("h-4 w-4", listingQuery.isFetching && "animate-spin")} />
                    Refresh
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => handleNavigate(listing?.parent_path || "")}
                    disabled={!listing?.parent_path}
                  >
                    <ArrowUp className="h-4 w-4" />
                    Up
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => openDialog("folder")}>
                    <FolderPlus className="h-4 w-4" />
                    New Folder
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => openDialog("file")}>
                    <FilePlus2 className="h-4 w-4" />
                    New File
                  </Button>
                  <Button size="sm" onClick={() => uploadInputRef.current?.click()}>
                    <Upload className="h-4 w-4" />
                    Upload
                  </Button>
                  {clipboardReady ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        void pasteInto(currentPath);
                      }}
                    >
                      <Check className="h-4 w-4" />
                      Paste
                    </Button>
                  ) : null}
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => {
                      setSettings({
                        startPath: currentPath,
                        preferredView: viewMode,
                      });
                      setSettingsOpen(true);
                    }}
                  >
                    <Settings2 className="h-4 w-4" />
                    Settings
                  </Button>
                </div>
              </div>

              <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                <div className="flex flex-wrap items-center gap-2 text-[13px] text-[var(--app-text-muted)]">
                  {breadcrumbs.map((crumb, index) => (
                    <div key={crumb.path || "root"} className="flex items-center gap-2">
                      {index > 0 ? <span className="text-[var(--app-border-strong)]">/</span> : null}
                      <button
                        type="button"
                        className={cn(
                          "rounded-[8px] px-2 py-1 transition-colors duration-150",
                          crumb.path === currentPath
                            ? "bg-[var(--app-accent-soft)] text-[var(--app-text)]"
                            : "hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]",
                        )}
                        onClick={() => handleNavigate(crumb.path)}
                      >
                        {crumb.label}
                      </button>
                    </div>
                  ))}
                </div>

                <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                  <div className="relative min-w-[220px]">
                    <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--app-text-muted)]" />
                    <Input
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                      placeholder="Search current folder"
                      className="pl-9"
                    />
                  </div>

                  <div className="flex items-center gap-1 rounded-[12px] border border-[var(--app-border)] bg-[var(--app-surface)] p-1">
                    <button
                      type="button"
                      className={cn(
                        "inline-flex h-8 w-8 items-center justify-center rounded-[8px] text-[var(--app-text-muted)] transition-colors duration-150",
                        viewMode === "list" && "bg-[var(--app-accent)] text-[#f7fbff]",
                      )}
                      onClick={() => setViewMode("list")}
                    >
                      <List className="h-4 w-4" />
                    </button>
                    <button
                      type="button"
                      className={cn(
                        "inline-flex h-8 w-8 items-center justify-center rounded-[8px] text-[var(--app-text-muted)] transition-colors duration-150",
                        viewMode === "grid" && "bg-[var(--app-accent)] text-[#f7fbff]",
                      )}
                      onClick={() => setViewMode("grid")}
                    >
                      <Grid2X2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div className="p-4 md:p-5">
              <div
                ref={browserRef}
                className={cn(
                  "relative overflow-hidden rounded-[16px] border border-[var(--app-border)] bg-[var(--app-bg-2)] select-none shadow-[var(--app-shadow)]",
                  rootDropActive && "ring-2 ring-[var(--app-accent)]/80",
                )}
                onMouseDown={beginMarquee}
                onContextMenu={handleBackgroundContextMenu}
                onDragOver={handleBrowserDragOver}
                onDragLeave={(event) => {
                  if (event.currentTarget.contains(event.relatedTarget as Node)) {
                    return;
                  }
                  setRootDropActive(false);
                }}
                onDrop={handleBrowserDrop}
              >
                <div className="flex items-center justify-between border-b border-[var(--app-border)] px-4 py-3">
                  <div>
                    <div className="text-[15px] font-medium text-[var(--app-text)]">Directory</div>
                    <div className="text-[12px] text-[var(--app-text-muted)]">
                      {normalizedSearch
                        ? `${filteredItems.length} matching item${filteredItems.length === 1 ? "" : "s"}`
                        : `${allItems.length} item${allItems.length === 1 ? "" : "s"}`}
                    </div>
                  </div>

                  {selectedPaths.length > 0 ? (
                    <div className="flex flex-wrap items-center gap-2">
                      <Button variant="secondary" size="sm" onClick={() => copySelection("copy")}>
                        <Copy className="h-4 w-4" />
                        Copy
                      </Button>
                      <Button variant="secondary" size="sm" onClick={() => copySelection("move")}>
                        <Scissors className="h-4 w-4" />
                        Cut
                      </Button>
                      {selectedItem?.type === "file" ? (
                        <>
                          <Button variant="secondary" size="sm" onClick={() => void openEditor(selectedItem.path)}>
                            <Pencil className="h-4 w-4" />
                            Edit
                          </Button>
                          <Button variant="secondary" size="sm" onClick={handleDownloadSelection}>
                            <Download className="h-4 w-4" />
                            Download
                          </Button>
                        </>
                      ) : null}
                      {selectedItem ? (
                        <Button variant="secondary" size="sm" onClick={() => openDialog("rename")}>
                          <Pencil className="h-4 w-4" />
                          Rename
                        </Button>
                      ) : null}
                      <Button variant="secondary" size="sm" onClick={handleDeleteSelection}>
                        <Trash2 className="h-4 w-4" />
                        Delete
                      </Button>
                    </div>
                  ) : null}
                </div>

                {listingQuery.isLoading ? (
                  <div className="flex min-h-[260px] items-center justify-center text-[13px] text-[var(--app-text-muted)]">
                    Loading directory...
                  </div>
                ) : listingQuery.isError ? (
                  <div className="flex min-h-[260px] items-center justify-center px-6 text-center text-[13px] text-[var(--app-danger)]">
                    {getErrorMessage(listingQuery.error, "Failed to load files.")}
                  </div>
                ) : filteredItems.length === 0 ? (
                  <EmptyState searchActive={Boolean(normalizedSearch)} />
                ) : viewMode === "list" ? (
                  <div className="overflow-x-auto">
                    <table className="min-w-full border-collapse text-left">
                      <thead className="bg-[var(--app-surface-muted)] text-[11px] uppercase tracking-[0.16em] text-[var(--app-text-muted)]">
                        <tr>
                          <th className="px-2.5 py-2 font-medium">Name</th>
                          <th className="px-2.5 py-2 font-medium">Type</th>
                          <th className="px-2.5 py-2 font-medium text-right">Size</th>
                          <th className="px-2.5 py-2 font-medium">Modified</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filteredItems.map((item) => {
                          const Icon = getItemIcon(item);
                          const isSelected = selectedSet.has(item.path);

                          return (
                            <tr
                              key={item.path}
                              data-selectable="1"
                              data-path={item.path}
                              data-type={item.type}
                              draggable={item.type !== "symlink"}
                              className={cn(
                                "cursor-pointer border-t border-[var(--app-border)] text-[13px] transition-colors duration-150 hover:bg-[var(--app-accent-soft)]/40",
                                isSelected && "bg-[var(--app-accent-soft)]/50",
                                dropTargetPath === item.path && item.type === "directory" && "ring-2 ring-inset ring-[var(--app-accent)]",
                              )}
                              onClick={(event) => handleSelectionClick(item, event)}
                              onDoubleClick={() => handleActivateItem(item)}
                              onContextMenu={(event) => handleItemContextMenu(event, item)}
                              onDragStart={(event) => handleInternalDragStart(item, event)}
                              onDragOver={(event) =>
                                item.type === "directory" ? handleDirectoryDragOver(item.path, event) : undefined
                              }
                              onDragLeave={() => {
                                if (dropTargetPath === item.path) {
                                  setDropTargetPath(null);
                                }
                              }}
                              onDrop={(event) =>
                                item.type === "directory" ? handleDirectoryDrop(item.path, event) : undefined
                              }
                            >
                              <td className="px-2.5 py-2">
                                <div className="flex items-center gap-2">
                                  <div className="flex h-6 w-6 items-center justify-center rounded-[6px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
                                    <Icon className="h-3.5 w-3.5" />
                                  </div>
                                  <div className="min-w-0 truncate font-medium text-[var(--app-text)]">{item.name}</div>
                                </div>
                              </td>
                              <td className="px-2.5 py-2 text-[var(--app-text-muted)]">{getItemLabel(item)}</td>
                              <td className="px-2.5 py-2 text-right text-[var(--app-text-muted)]">
                                {item.type === "file" ? formatBytes(item.size) : "-"}
                              </td>
                              <td className="px-2.5 py-2 text-[var(--app-text-muted)]">
                                {formatDateTime(item.modified_at)}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="flex flex-wrap gap-3 p-4">
                    {filteredItems.map((item) => {
                      const Icon = getItemIcon(item);
                      const isSelected = selectedSet.has(item.path);
                      const tone = getGridPreviewTone(item);

                      return (
                        <button
                          key={item.path}
                          type="button"
                          data-selectable="1"
                          data-path={item.path}
                          data-type={item.type}
                          draggable={item.type !== "symlink"}
                          className={cn(
                            "group flex w-[8.4rem] shrink-0 flex-col items-center rounded-[18px] border px-3 py-2.5 text-center transition-colors duration-150",
                            isSelected
                              ? "border-[var(--app-accent)] bg-[var(--app-accent-soft)] shadow-[0_0_0_1px_rgba(29,78,216,0.18),0_18px_34px_rgba(37,99,235,0.12)]"
                              : "border-[var(--app-border)] bg-[var(--app-surface-muted)] shadow-[0_10px_24px_rgba(15,23,42,0.06)] hover:border-[var(--app-border-strong)]",
                            dropTargetPath === item.path &&
                              item.type === "directory" &&
                              "border-[var(--app-accent)] bg-[var(--app-accent-soft)] shadow-[0_0_0_1px_rgba(29,78,216,0.18),0_18px_34px_rgba(37,99,235,0.12)]",
                          )}
                          style={{ minHeight: 152 }}
                          onClick={(event) => handleSelectionClick(item, event)}
                          onDoubleClick={() => handleActivateItem(item)}
                          onContextMenu={(event) => handleItemContextMenu(event, item)}
                          onDragStart={(event) => handleInternalDragStart(item, event)}
                          onDragOver={(event) =>
                            item.type === "directory" ? handleDirectoryDragOver(item.path, event) : undefined
                          }
                          onDragLeave={() => {
                            if (dropTargetPath === item.path) {
                              setDropTargetPath(null);
                            }
                          }}
                          onDrop={(event) =>
                            item.type === "directory" ? handleDirectoryDrop(item.path, event) : undefined
                          }
                        >
                          <div
                            className={cn(
                              "relative mb-2.5 flex aspect-square w-full max-w-[3.8rem] items-center justify-center overflow-hidden rounded-[15px] border",
                              tone.frame,
                            )}
                          >
                            <Icon className="relative z-10 h-7 w-7" />
                          </div>
                          <div
                            className="w-full overflow-hidden text-[13px] font-semibold leading-5 text-[var(--app-text)]"
                            style={{
                              display: "-webkit-box",
                              WebkitLineClamp: 2,
                              WebkitBoxOrient: "vertical",
                            }}
                          >
                            {item.name}
                          </div>
                          <div className="mt-1.5 text-[11px] text-[var(--app-text-muted)]">
                            {item.type === "file" ? formatBytes(item.size) : "Directory"}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                )}

                {marqueeRect ? (
                  <div
                    className="pointer-events-none fixed z-40 rounded-[8px] border border-[var(--app-accent)] bg-blue-100/60"
                    style={marqueeRect}
                  />
                ) : null}

                {contextMenu && contextMenuItems.length > 0 ? (
                  <div
                    ref={contextMenuRef}
                    data-context-menu="1"
                    className="fixed z-50 min-w-[240px] rounded-[14px] border border-[var(--app-border)] bg-[var(--app-surface-elev)] p-2.5 shadow-[0_18px_35px_rgba(15,23,42,0.16)] backdrop-blur-sm select-none"
                    style={contextMenuStyle ?? undefined}
                  >
                    <div className="space-y-1">
                      {contextMenuItems.map((item) => {
                        const Icon = item.icon;

                        return (
                          <button
                            key={item.key}
                            type="button"
                            className="flex w-full items-center gap-3 rounded-[10px] px-3.5 py-2.5 text-left text-[15px] text-[var(--app-text)] transition-colors duration-150 hover:bg-[var(--app-accent-soft)]"
                            onClick={() => {
                              setContextMenu(null);
                              item.handler();
                            }}
                          >
                            <Icon className="h-[18px] w-[18px] text-[var(--app-text-muted)]" />
                            {item.label}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                ) : null}
              </div>
            </div>
          </section>
        </div>
      </div>

      <input
        ref={uploadInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => {
          handleUploadSelection(event.target.files);
          event.target.value = "";
        }}
      />

      <Dialog open={dialogMode !== null} onOpenChange={(open) => (!open ? setDialogMode(null) : null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription>
              {dialogMode === "rename"
                ? "Update the selected name."
                : `Create this inside ${pathLabel(listing?.path || currentPath)}.`}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void submitDialog(event)}>
            <div className="space-y-2">
              <label className="text-[13px] font-medium text-[var(--app-text)]" htmlFor="files-dialog-name">
                Name
              </label>
              <Input
                id="files-dialog-name"
                value={dialogValue}
                onChange={(event) => setDialogValue(event.target.value)}
                placeholder={dialogMode === "file" ? "new-file.txt" : "new-folder"}
                autoFocus
              />
            </div>
            <DialogFooter>
              <div className="text-[12px] text-[var(--app-text-muted)]">
                {listing?.absolute_path || listing?.root_path}
              </div>
              <Button type="submit" disabled={isMutating || !dialogValue.trim()}>
                {dialogMode === "rename" ? "Rename" : "Create"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>File Manager Settings</DialogTitle>
            <DialogDescription>
              Resume the last opened folder automatically, with an optional fallback start folder and preferred view.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-[13px] font-medium text-[var(--app-text)]" htmlFor="files-settings-path">
                Start folder
              </label>
              <Input
                id="files-settings-path"
                value={settings.startPath}
                onChange={(event) =>
                  setSettings((current) => ({ ...current, startPath: event.target.value.trim() }))
                }
                placeholder="Leave blank for the root"
              />
              <p className="text-[12px] text-[var(--app-text-muted)]">
                Current root: {listing?.root_path || "Loading..."} Leave blank to use the last opened folder or root.
              </p>
            </div>

            <div className="space-y-2">
              <div className="text-[13px] font-medium text-[var(--app-text)]">Preferred view</div>
              <div className="flex items-center gap-2">
                <Button
                  variant={settings.preferredView === "list" ? "default" : "secondary"}
                  size="sm"
                  onClick={() => setSettings((current) => ({ ...current, preferredView: "list" }))}
                >
                  <List className="h-4 w-4" />
                  List
                </Button>
                <Button
                  variant={settings.preferredView === "grid" ? "default" : "secondary"}
                  size="sm"
                  onClick={() => setSettings((current) => ({ ...current, preferredView: "grid" }))}
                >
                  <Grid2X2 className="h-4 w-4" />
                  Grid
                </Button>
              </div>
            </div>
          </div>

          <DialogFooter>
            <div className="text-[12px] text-[var(--app-text-muted)]">Saved locally in the browser.</div>
            <Button
              onClick={() => {
                setViewMode(settings.preferredView);
                handleNavigate(settings.startPath);
                setSettingsOpen(false);
                setFlash({ tone: "success", text: "File manager settings updated." });
              }}
            >
              Save Settings
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Sheet
        open={editorOpen}
        onOpenChange={(open) => {
          setEditorOpen(open);
          if (!open) {
            setEditorPath("");
            setEditorName("");
            setEditorContent("");
            setEditorOriginalContent("");
            setEditorMeta(null);
          }
        }}
      >
        <SheetContent side="left" className="!w-[60vw] !max-w-none sm:!max-w-none gap-0 p-0">
          <SheetHeader className="gap-1 border-b border-[var(--app-border)] px-5 py-4">
            <SheetTitle>{editorName || "Editor"}</SheetTitle>
            <SheetDescription>
              {editorMeta
                ? `${formatBytes(editorMeta.size)} / ${formatDateTime(editorMeta.modifiedAt)}`
                : "Loading file contents..."}
            </SheetDescription>
          </SheetHeader>

          <div className="flex min-h-0 flex-1 flex-col px-5 py-4">
            <div className="mb-3 rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 text-[12px] text-[var(--app-text-muted)]">
              {editorPath}
            </div>
            <Suspense
              fallback={
                <div className="flex min-h-0 flex-1 items-center justify-center rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[13px] text-[var(--app-text-muted)]">
                  Loading editor...
                </div>
              }
            >
              <FileAceEditor
                path={editorPath || editorName}
                value={editorContent}
                onChange={setEditorContent}
                readOnly={editorBusy}
              />
            </Suspense>
          </div>

          <SheetFooter className="mt-0 flex-row items-center justify-between border-t border-[var(--app-border)] px-5 py-4">
            <div className="text-[12px] text-[var(--app-text-muted)]">
              {editorDirty ? "Unsaved changes" : "No pending changes"}
            </div>
            <Button onClick={() => void saveEditor()} disabled={editorBusy || !editorDirty}>
              {editorBusy ? "Saving..." : "Save"}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  );
}
