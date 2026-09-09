package archiveutil

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func ContentType(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return "application/zip"
	case strings.HasSuffix(name, ".gz"), strings.HasSuffix(name, ".gzip"), strings.HasSuffix(name, ".tgz"):
		return "application/gzip"
	case strings.HasSuffix(name, ".zst"), strings.HasSuffix(name, ".zstd"), strings.HasSuffix(name, ".tzst"):
		return "application/zstd"
	default:
		return "application/octet-stream"
	}
}

func Supported(name string) bool {
	return ContentType(name) != "application/octet-stream"
}

// OpenCompressed detects gzip or Zstandard without taking ownership of source.
func OpenCompressed(source io.Reader) (io.ReadCloser, error) {
	reader := bufio.NewReader(source)
	header, err := reader.Peek(4)
	if err != nil {
		return nil, err
	}
	switch {
	case header[0] == 0x1f && header[1] == 0x8b:
		return gzip.NewReader(reader)
	case string(header) == "\x28\xb5\x2f\xfd", header[0]&0xf0 == 0x50 && string(header[1:]) == "\x2a\x4d\x18":
		decoder, err := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(256<<20))
		if err != nil {
			return nil, err
		}
		return decoder.IOReadCloser(), nil
	default:
		return nil, errors.New("unsupported archive compression")
	}
}
