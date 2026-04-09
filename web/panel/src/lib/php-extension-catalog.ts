export type PHPExtensionCatalogEntry = {
  id: string;
  label: string;
  aliases?: string[];
};

export const phpExtensionCatalog: PHPExtensionCatalogEntry[] = [
  { id: "oncube", label: "onCube", aliases: ["ioncube", "ioncube loader"] },
  { id: "fileinfo", label: "fileinfo" },
  { id: "opcache", label: "opcache", aliases: ["zend opcache"] },
  { id: "memcached", label: "memcached" },
  { id: "redis", label: "redis" },
  { id: "mcrypt", label: "mcrypt" },
  { id: "apcu", label: "apcu" },
  { id: "imagemagick", label: "imagemagick", aliases: ["imagick"] },
  { id: "xdebug", label: "xdebug" },
  { id: "imap", label: "imap" },
  { id: "exif", label: "exif" },
  { id: "intl", label: "intl" },
  { id: "xsl", label: "xsl" },
  { id: "swoole4", label: "Swoole4" },
  { id: "swoole5", label: "Swoole5" },
  { id: "swoole6", label: "Swoole6" },
  { id: "xlswriter", label: "xlswriter" },
  { id: "oci8", label: "oci8" },
  { id: "pdo_oci", label: "pdo_oci", aliases: ["pdooci"] },
  { id: "swow", label: "swow" },
  { id: "pdo_sqlsrv", label: "pdo_sqlsrv", aliases: ["pdosqlsrv"] },
  { id: "sqlsrv", label: "sqlsrv" },
  { id: "rdkafka", label: "rdkafka", aliases: ["rdkakfa"] },
  { id: "yaf", label: "yaf" },
  { id: "php_mongodb", label: "php_mongodb", aliases: ["mongodb", "phpmongodb"] },
  { id: "yac", label: "yac" },
  { id: "sg11", label: "sg11" },
  { id: "sg14", label: "sg14" },
  { id: "sg15", label: "sg15" },
  { id: "sg16", label: "sg16" },
  { id: "xload", label: "xload" },
  { id: "pgsql", label: "pgsql" },
  { id: "ssh2", label: "ssh2" },
  { id: "grpc", label: "grpc" },
  { id: "xhprof", label: "xhprof" },
  { id: "protobuf", label: "protobuf" },
  { id: "pdo_pgsql", label: "pdo_pgsql", aliases: ["pdopgsql"] },
  { id: "readline", label: "readline" },
  { id: "snmp", label: "snmp" },
  { id: "ldap", label: "ldap" },
  { id: "enchant", label: "enchant" },
  { id: "pspell", label: "pspell" },
  { id: "bz2", label: "bz2" },
  { id: "sysvshm", label: "sysvshm" },
  { id: "calendar", label: "calendar" },
  { id: "gmp", label: "gmp" },
  { id: "sysvmsg", label: "sysvmsg" },
  { id: "igbinary", label: "igbinary" },
  { id: "zmq", label: "zmq" },
  { id: "zstd", label: "zstd" },
  { id: "smbclient", label: "smbclient" },
  { id: "event", label: "event" },
  { id: "mailparse", label: "mailparse" },
  { id: "yaml", label: "yaml" },
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
