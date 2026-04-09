export type PHPExtensionCatalogEntry = {
  id: string;
  label: string;
  aliases?: string[];
  installId?: string;
  installSupported?: boolean;
};

export const phpExtensionCatalog: PHPExtensionCatalogEntry[] = [
  {
    id: "ioncube",
    label: "ionCube",
    aliases: ["oncube", "ioncube loader", "ioncubeloader"],
    installSupported: false,
  },
  { id: "fileinfo", label: "fileinfo", installSupported: false },
  { id: "opcache", label: "opcache", aliases: ["zend opcache", "zendopcache"], installSupported: false },
  { id: "memcached", label: "memcached" },
  { id: "redis", label: "redis" },
  { id: "mcrypt", label: "mcrypt" },
  { id: "apcu", label: "apcu" },
  { id: "imagemagick", label: "imagemagick", aliases: ["imagick"] },
  { id: "xdebug", label: "xdebug" },
  { id: "imap", label: "imap", installSupported: false },
  { id: "exif", label: "exif", installSupported: false },
  { id: "intl", label: "intl", installSupported: false },
  { id: "xsl", label: "xsl", installSupported: false },
  { id: "swoole", label: "swoole", aliases: ["swoole4", "swoole5", "swoole6"], installId: "swoole" },
  { id: "xlswriter", label: "xlswriter" },
  { id: "oci8", label: "oci8", installSupported: false },
  { id: "pdo_oci", label: "pdo_oci", aliases: ["pdooci"], installSupported: false },
  { id: "swow", label: "swow" },
  { id: "pdo_sqlsrv", label: "pdo_sqlsrv", aliases: ["pdosqlsrv"], installSupported: false },
  { id: "sqlsrv", label: "sqlsrv", installSupported: false },
  { id: "rdkafka", label: "rdkafka", aliases: ["rdkakfa"] },
  { id: "yaf", label: "yaf", installSupported: false },
  { id: "php_mongodb", label: "php_mongodb", aliases: ["mongodb", "phpmongodb"], installId: "mongodb" },
  { id: "yac", label: "yac", installSupported: false },
  { id: "sg11", label: "sg11", aliases: ["sourceguardian11"], installSupported: false },
  { id: "sg14", label: "sg14", aliases: ["sourceguardian14"], installSupported: false },
  { id: "sg15", label: "sg15", aliases: ["sourceguardian15"], installSupported: false },
  { id: "sg16", label: "sg16", aliases: ["sourceguardian16"], installSupported: false },
  { id: "xload", label: "xload", installSupported: false },
  { id: "pgsql", label: "pgsql", installSupported: false },
  { id: "ssh2", label: "ssh2", installSupported: false },
  { id: "grpc", label: "grpc" },
  { id: "xhprof", label: "xhprof", installSupported: false },
  { id: "protobuf", label: "protobuf" },
  { id: "pdo_pgsql", label: "pdo_pgsql", aliases: ["pdopgsql"], installSupported: false },
  { id: "readline", label: "readline", installSupported: false },
  { id: "snmp", label: "snmp", installSupported: false },
  { id: "ldap", label: "ldap", installSupported: false },
  { id: "enchant", label: "enchant", installSupported: false },
  { id: "pspell", label: "pspell", installSupported: false },
  { id: "bz2", label: "bz2", installSupported: false },
  { id: "sysvshm", label: "sysvshm", installSupported: false },
  { id: "calendar", label: "calendar", installSupported: false },
  { id: "gmp", label: "gmp", installSupported: false },
  { id: "sysvmsg", label: "sysvmsg", installSupported: false },
  { id: "igbinary", label: "igbinary" },
  { id: "zmq", label: "zmq", installSupported: false },
  { id: "zstd", label: "zstd" },
  { id: "smbclient", label: "smbclient", installSupported: false },
  { id: "event", label: "event", installSupported: false },
  { id: "mailparse", label: "mailparse" },
  { id: "yaml", label: "yaml", installSupported: false },
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
