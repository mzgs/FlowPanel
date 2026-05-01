import {
  ArrowUp as LucideArrowUp,
  Bell as LucideBell,
  Check as LucideCheck,
  ChevronDown as LucideChevronDown,
  ChevronRight as LucideChevronRight,
  ChevronUp as LucideChevronUp,
  Circle as LucideCircle,
  CircleCheck as LucideCircleCheck,
  Clipboard as LucideClipboard,
  Clock as LucideClock,
  Cloud as LucideCloud,
  Container as LucideContainer,
  Copy as LucideCopy,
  Database as LucideDatabase,
  Download as LucideDownload,
  EllipsisVertical as LucideEllipsisVertical,
  ExternalLink as LucideExternalLink,
  Eye as LucideEye,
  EyeOff as LucideEyeOff,
  File as LucideFile,
  FileCode as LucideFileCode,
  FilePlus as LucideFilePlus,
  FileSymlink as LucideFileSymlink,
  Folder as LucideFolder,
  FolderOpen as LucideFolderOpen,
  FolderPlus as LucideFolderPlus,
  GitBranch as LucideGitBranch,
  Globe as LucideGlobe,
  Grid2x2 as LucideGrid2x2,
  HardDrive as LucideHardDrive,
  LayoutDashboard as LucideLayoutDashboard,
  List as LucideList,
  LogOut as LucideLogOut,
  LoaderCircle as LucideLoaderCircle,
  Monitor as LucideMonitor,
  Package as LucidePackage,
  PanelLeftClose as LucidePanelLeftClose,
  Pencil as LucidePencil,
  Play as LucidePlay,
  Plus as LucidePlus,
  RefreshCw as LucideRefreshCw,
  RotateCcw as LucideRotateCcw,
  Scissors as LucideScissors,
  Search as LucideSearch,
  Server as LucideServer,
  Settings as LucideSettings,
  ShieldCheck as LucideShieldCheck,
  SlidersHorizontal as LucideSlidersHorizontal,
  SlidersVertical as LucideSlidersVertical,
  Square as LucideSquare,
  SquareTerminal as LucideSquareTerminal,
  Star as LucideStar,
  TimerReset as LucideTimerReset,
  Trash2 as LucideTrash2,
  Upload as LucideUpload,
  UserCog as LucideUserCog,
  Wrench as LucideWrench,
  X as LucideX,
  type LucideIcon,
} from "lucide-react";
import { forwardRef, type ComponentProps } from "react";

function icon(Icon: LucideIcon) {
  return forwardRef<SVGSVGElement, ComponentProps<typeof Icon>>((props, ref) => (
    <Icon ref={ref} {...props} />
  ));
}

export const Adjustments = icon(LucideSlidersVertical);
export const AdjustmentsHorizontal = icon(LucideSlidersHorizontal);
export const ArrowUp = icon(LucideArrowUp);
export const Bell = icon(LucideBell);
export const BrandWordpress = icon(LucideGlobe);
export const Check = icon(LucideCheck);
export const CheckIcon = Check;
export const ChevronDownIcon = icon(LucideChevronDown);
export const ChevronRight = icon(LucideChevronRight);
export const ChevronUpIcon = icon(LucideChevronUp);
export const CircleCheck = icon(LucideCircleCheck);
export const CircleIcon = icon(LucideCircle);
export const Clipboard = icon(LucideClipboard);
export const Clock = icon(LucideClock);
export const Copy = icon(LucideCopy);
export const Database = icon(LucideDatabase);
export const Docker = icon(LucideContainer);
export const DotsVertical = icon(LucideEllipsisVertical);
export const Download = icon(LucideDownload);
export const ExternalLink = icon(LucideExternalLink);
export const Eye = icon(LucideEye);
export const EyeOff = icon(LucideEyeOff);
export const File = icon(LucideFile);
export const FileCode2 = icon(LucideFileCode);
export const FilePlus2 = icon(LucideFilePlus);
export const FileSymlink = icon(LucideFileSymlink);
export const Folder = icon(LucideFolder);
export const FolderOpen = icon(LucideFolderOpen);
export const FolderPlus = icon(LucideFolderPlus);
export const GitBranch = icon(LucideGitBranch);
export const Globe = icon(LucideGlobe);
export const GoogleDrive = icon(LucideCloud);
export const Grid2X2 = icon(LucideGrid2x2);
export const HardDrive = icon(LucideHardDrive);
export const LayoutDashboard = icon(LucideLayoutDashboard);
export const List = icon(LucideList);
export const LogOut = icon(LucideLogOut);
export const LoaderCircle = icon(LucideLoaderCircle);
export const Monitor = icon(LucideMonitor);
export const Package = icon(LucidePackage);
export const PanelLeftIcon = icon(LucidePanelLeftClose);
export const Pencil = icon(LucidePencil);
export const PlayerPlay = icon(LucidePlay);
export const PlayerPlayFilled = icon(LucidePlay);
export const PlayerStop = icon(LucideSquare);
export const Plus = icon(LucidePlus);
export const RefreshCw = icon(LucideRefreshCw);
export const RotateCcw = icon(LucideRotateCcw);
export const Scissors = icon(LucideScissors);
export const Search = icon(LucideSearch);
export const Server = icon(LucideServer);
export const Settings = icon(LucideSettings);
export const ShieldCheck = icon(LucideShieldCheck);
export const Star = icon(LucideStar);
export const TerminalSquare = icon(LucideSquareTerminal);
export const TimerReset = icon(LucideTimerReset);
export const Trash2 = icon(LucideTrash2);
export const Upload = icon(LucideUpload);
export const UserCog = icon(LucideUserCog);
export const World = icon(LucideGlobe);
export const Wrench = icon(LucideWrench);
export const XIcon = icon(LucideX);
