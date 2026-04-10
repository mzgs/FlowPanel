export type PHPExtensionCatalogEntry = {
  id: string;
  label: string;
  aliases?: string[];
  installId?: string;
  installSupported?: boolean;
};

export const phpExtensionCatalog: PHPExtensionCatalogEntry[] = [
  { id: "amqp", label: "amqp", installSupported: true },
  { id: "apcu", label: "apcu", installSupported: true },
  { id: "calendar", label: "calendar", installSupported: false },
  { id: "ds", label: "ds", installSupported: false },
  { id: "event", label: "event", installSupported: true },
  { id: "exif", label: "exif", installSupported: false },
  { id: "fileinfo", label: "fileinfo", installSupported: false },
  { id: "grpc", label: "grpc", installSupported: false },
  { id: "igbinary", label: "igbinary", installSupported: true },
  { id: "imagemagick", label: "imagemagick", aliases: ["imagick"], installSupported: true },
  { id: "imap", label: "imap", installSupported: true },
  {
    id: "ioncube",
    label: "ionCube",
    aliases: ["oncube", "ioncube loader", "ioncubeloader"],
    installSupported: false,
  },
  { id: "intl", label: "intl", installSupported: true },
  { id: "mailparse", label: "mailparse", installSupported: true },
  { id: "mcrypt", label: "mcrypt", installSupported: true },
  { id: "memcached", label: "memcached", installSupported: true },
  { id: "msgpack", label: "msgpack", installSupported: true },
  { id: "oci8", label: "oci8", installSupported: false },
  { id: "opcache", label: "opcache", aliases: ["zend opcache", "zendopcache"], installSupported: false },
  { id: "openswoole", label: "openswoole", installSupported: false },
  { id: "parallel", label: "parallel", installSupported: false },
  { id: "pcov", label: "pcov", installSupported: true },
  { id: "pdo_mysql", label: "pdo_mysql", installSupported: true },
  { id: "pdo_oci", label: "pdo_oci", aliases: ["pdooci"], installSupported: false },
  { id: "pdo_pgsql", label: "pdo_pgsql", aliases: ["pdopgsql"], installSupported: true },
  { id: "pdo_sqlite", label: "pdo_sqlite", installSupported: true },
  { id: "pdo_sqlsrv", label: "pdo_sqlsrv", aliases: ["pdosqlsrv"], installSupported: false },
  { id: "pgsql", label: "pgsql", installSupported: true },
  { id: "php_mongodb", label: "php_mongodb", aliases: ["mongodb", "phpmongodb"], installId: "mongodb", installSupported: true },
  { id: "protobuf", label: "protobuf", installSupported: false },
  { id: "rdkafka", label: "rdkafka", aliases: ["rdkakfa"], installSupported: false },
  { id: "redis", label: "redis", installSupported: true },
  { id: "snmp", label: "snmp", installSupported: true },
  { id: "swoole", label: "swoole", aliases: ["swoole4", "swoole5", "swoole6"], installId: "swoole", installSupported: false },
  { id: "swow", label: "swow", installSupported: false },
  { id: "timezonedb", label: "timezonedb", installSupported: true },
  { id: "uuid", label: "uuid", installSupported: true },
  { id: "xdebug", label: "xdebug", installSupported: true },
  { id: "xlswriter", label: "xlswriter", installSupported: false },
  { id: "yaml", label: "yaml", installSupported: true },
  { id: "zip", label: "zip", installSupported: true },
  { id: "zstd", label: "zstd", installSupported: false },
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
