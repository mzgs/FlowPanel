import type { PHPSettings } from "@/api/php";

export const defaultPHPErrorReporting = "E_ALL & ~E_NOTICE & ~E_DEPRECATED";
export const defaultDisabledFunctions =
  "exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec";

export const phpErrorReportingOptions = [
  { value: "E_ALL", label: "E_ALL" },
  { value: "E_ALL & ~E_NOTICE", label: "E_ALL & ~E_NOTICE" },
  { value: "E_ALL & ~E_DEPRECATED", label: "E_ALL & ~E_DEPRECATED" },
  {
    value: defaultPHPErrorReporting,
    label: defaultPHPErrorReporting,
  },
] as const;

const phpErrorReportingValues = new Set(
  phpErrorReportingOptions.map((option) => option.value),
);

export const emptyPHPSettings: PHPSettings = {
  max_execution_time: "",
  max_input_time: "",
  memory_limit: "",
  post_max_size: "",
  file_uploads: "On",
  upload_max_filesize: "",
  max_file_uploads: "",
  default_socket_timeout: "",
  error_reporting: defaultPHPErrorReporting,
  display_errors: "Off",
  disable_functions: defaultDisabledFunctions,
};

export function normalizePHPErrorReporting(value?: string | null) {
  const normalized = value?.trim();
  if (!normalized || !phpErrorReportingValues.has(normalized)) {
    return defaultPHPErrorReporting;
  }

  return normalized;
}

export function toPHPSettingsForm(settings?: PHPSettings | null): PHPSettings {
  return {
    max_execution_time: settings?.max_execution_time ?? "",
    max_input_time: settings?.max_input_time ?? "",
    memory_limit: settings?.memory_limit ?? "",
    post_max_size: settings?.post_max_size ?? "",
    file_uploads: settings?.file_uploads ?? "On",
    upload_max_filesize: settings?.upload_max_filesize ?? "",
    max_file_uploads: settings?.max_file_uploads ?? "",
    default_socket_timeout: settings?.default_socket_timeout ?? "",
    error_reporting: normalizePHPErrorReporting(settings?.error_reporting),
    display_errors: settings?.display_errors ?? "Off",
    disable_functions:
      settings?.disable_functions ?? defaultDisabledFunctions,
  };
}

export function mergePHPSettingsForm(
  base?: PHPSettings | null,
  overrides?: PHPSettings | null,
): PHPSettings {
  const normalizedBase = toPHPSettingsForm(base);
  if (!overrides) {
    return normalizedBase;
  }

  return {
    max_execution_time:
      overrides.max_execution_time || normalizedBase.max_execution_time,
    max_input_time: overrides.max_input_time || normalizedBase.max_input_time,
    memory_limit: overrides.memory_limit || normalizedBase.memory_limit,
    post_max_size: overrides.post_max_size || normalizedBase.post_max_size,
    file_uploads: overrides.file_uploads || normalizedBase.file_uploads,
    upload_max_filesize:
      overrides.upload_max_filesize || normalizedBase.upload_max_filesize,
    max_file_uploads: overrides.max_file_uploads || normalizedBase.max_file_uploads,
    default_socket_timeout:
      overrides.default_socket_timeout || normalizedBase.default_socket_timeout,
    error_reporting: normalizePHPErrorReporting(
      overrides.error_reporting || normalizedBase.error_reporting,
    ),
    display_errors: overrides.display_errors || normalizedBase.display_errors,
    disable_functions:
      overrides.disable_functions || normalizedBase.disable_functions,
  };
}

export function samePHPSettings(left: PHPSettings, right: PHPSettings) {
  return (
    left.max_execution_time === right.max_execution_time &&
    left.max_input_time === right.max_input_time &&
    left.memory_limit === right.memory_limit &&
    left.post_max_size === right.post_max_size &&
    left.file_uploads === right.file_uploads &&
    left.upload_max_filesize === right.upload_max_filesize &&
    left.max_file_uploads === right.max_file_uploads &&
    left.default_socket_timeout === right.default_socket_timeout &&
    left.error_reporting === right.error_reporting &&
    left.display_errors === right.display_errors &&
    left.disable_functions === right.disable_functions
  );
}
