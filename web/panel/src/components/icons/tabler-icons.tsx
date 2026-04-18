import * as TablerIcons from "@tabler/icons-react";
import type { ComponentType } from "react";

type IconProps = {
  className?: string;
  stroke?: number | string;
  size?: number | string;
};

type IconComponent = ComponentType<IconProps>;

const icons = TablerIcons as unknown as Record<string, IconComponent>;
const fallback = icons.IconCircle;

function pick(...names: string[]): IconComponent {
  return names.map((name) => icons[name]).find(Boolean) ?? fallback;
}

export const ArrowUp = pick("IconArrowUp");
export const Bell = pick("IconBell");
export const BrandWordpress = pick("IconBrandWordpress", "IconBrandWordpressSimple", "IconBrandWordpressFilled");
export const Check = pick("IconCheck");
export const CheckIcon = pick("IconCheck");
export const Clipboard = pick("IconClipboard", "IconClipboardText");
export const Clock = pick("IconClock");
export const ChevronDownIcon = pick("IconChevronDown");
export const ChevronRight = pick("IconChevronRight");
export const ChevronUpIcon = pick("IconChevronUp");
export const CircleCheck = pick("IconCircleCheck");
export const CircleIcon = pick("IconCircle");
export const Copy = pick("IconCopy");
export const Database = pick("IconDatabase");
export const Download = pick("IconDownload");
export const Eye = pick("IconEye");
export const EyeOff = pick("IconEyeOff");
export const ExternalLink = pick("IconExternalLink");
export const File = pick("IconFile");
export const FileCode2 = pick("IconFileCode2", "IconFileCode");
export const FilePlus2 = pick("IconFilePlus2", "IconFilePlus");
export const FileSymlink = pick("IconFileSymlink");
export const Folder = pick("IconFolder");
export const FolderOpen = pick("IconFolderOpen");
export const FolderPlus = pick("IconFolderPlus");
export const GoogleDrive = pick("IconBrandGoogleDrive", "IconBrandGoogle");
export const Globe = pick("IconGlobe");
export const GitBranch = pick("IconGitBranch", "IconBrandGit");
export const Grid2X2 = pick("IconLayoutGrid", "IconGridDots");
export const HardDrive = pick("IconDeviceFloppy", "IconArchive", "IconDatabaseExport");
export const LayoutDashboard = pick("IconLayoutDashboard");
export const List = pick("IconList");
export const LoaderCircle = pick("IconLoader2", "IconLoader");
export const Monitor = pick("IconDeviceDesktop", "IconMonitor");
export const Package = pick("IconPackage");
export const PanelLeftIcon = pick("IconLayoutSidebarLeftCollapse", "IconPanelLeft");
export const Pencil = pick("IconPencil");
export const PlayerPlay = pick("IconPlayerPlay", "IconPlay");
export const PlayerPlayFilled = pick("IconPlayerPlayFilled", "IconPlayerPlay", "IconPlay");
export const PlayerStop = pick("IconPlayerStopFilled", "IconPlayerStop", "IconSquare");
export const Plus = pick("IconPlus");
export const RefreshCw = pick("IconRefresh", "IconRefreshDot");
export const RotateCcw = pick("IconRotateClockwise2", "IconRotate2");
export const Scissors = pick("IconCut");
export const Search = pick("IconSearch");
export const Server = pick("IconServer");
export const Settings = pick("IconSettings");
export const ShieldCheck = pick("IconShieldCheck");
export const TerminalSquare = pick("IconTerminal2");
export const TimerReset = pick("IconClockRefresh", "IconRefresh");
export const Trash2 = pick("IconTrash");
export const Upload = pick("IconUpload");
export const UserCog = pick("IconUserCog");
export const Wrench = pick("IconTool");
export const World = pick("IconWorld", "IconGlobe");
export const XIcon = pick("IconX");
