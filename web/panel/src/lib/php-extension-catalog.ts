export type PHPExtensionCatalogEntry = {
  id: string;
  label: string;
  aliases?: string[];
  installId?: string;
  installSupported?: boolean;
  requiredDependencies?: PHPExtensionRequiredDependencies;
};

export type PHPExtensionRequiredDependencies = {
  apt?: string[];
  dnf?: string[];
  homebrew?: string[];
  yum?: string[];
};

export const phpExtensionCatalog: PHPExtensionCatalogEntry[] = [
  {
    id: "amqp",
    label: "amqp",
    requiredDependencies: {
      apt: ["librabbitmq-dev"],
      dnf: ["librabbitmq-devel"],
      homebrew: ["rabbitmq-c"],
      yum: ["librabbitmq-devel"],
    },
  },
  { id: "apcu", label: "apcu" },
  { id: "calendar", label: "calendar", installSupported: false },
  { id: "ds", label: "ds" },
  { id: "event", label: "event" },
  { id: "exif", label: "exif", installSupported: false },
  { id: "fileinfo", label: "fileinfo", installSupported: false },
  { id: "grpc", label: "grpc" },
  { id: "igbinary", label: "igbinary" },
  { id: "imagemagick", label: "imagemagick", aliases: ["imagick"] },
  { id: "imap", label: "imap", installSupported: false },
  {
    id: "ioncube",
    label: "ionCube",
    aliases: ["oncube", "ioncube loader", "ioncubeloader"],
    installSupported: false,
  },
  { id: "intl", label: "intl", installSupported: false },
  { id: "mailparse", label: "mailparse" },
  { id: "mcrypt", label: "mcrypt" },
  { id: "memcached", label: "memcached" },
  { id: "msgpack", label: "msgpack" },
  { id: "oci8", label: "oci8" },
  { id: "opcache", label: "opcache", aliases: ["zend opcache", "zendopcache"], installSupported: false },
  { id: "openswoole", label: "openswoole" },
  { id: "parallel", label: "parallel" },
  { id: "pcov", label: "pcov" },
  { id: "pdo_mysql", label: "pdo_mysql", installSupported: false },
  { id: "pdo_oci", label: "pdo_oci", aliases: ["pdooci"] },
  { id: "pdo_pgsql", label: "pdo_pgsql", aliases: ["pdopgsql"], installSupported: false },
  { id: "pdo_sqlite", label: "pdo_sqlite", installSupported: false },
  { id: "pdo_sqlsrv", label: "pdo_sqlsrv", aliases: ["pdosqlsrv"], installSupported: false },
  { id: "pgsql", label: "pgsql", installSupported: false },
  { id: "php_mongodb", label: "php_mongodb", aliases: ["mongodb", "phpmongodb"], installId: "mongodb" },
  { id: "protobuf", label: "protobuf" },
  { id: "rdkafka", label: "rdkafka", aliases: ["rdkakfa"] },
  { id: "redis", label: "redis" },
  { id: "swoole", label: "swoole", aliases: ["swoole4", "swoole5", "swoole6"], installId: "swoole" },
  { id: "swow", label: "swow" },
  { id: "timezonedb", label: "timezonedb" },
  { id: "uuid", label: "uuid" },
  { id: "xdebug", label: "xdebug" },
  { id: "xlswriter", label: "xlswriter" },
  { id: "yaml", label: "yaml" },
  { id: "zip", label: "zip" },
  { id: "zstd", label: "zstd" },
];

export function normalizePHPExtensionToken(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "");
}

export function isPHPExtensionInstalled(
  entry: PHPExtensionCatalogEntry,
  installedExtensions: string[],
) {
  const installed = new Set(installedExtensions.map(normalizePHPExtensionToken));
  const candidates = [entry.id, ...(entry.aliases ?? [])].map(normalizePHPExtensionToken);
  return candidates.some((candidate) => installed.has(candidate));
}
