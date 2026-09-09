package archiveutil

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"errors"
	"io"
	"math"
	"os"

	"github.com/shirou/gopsutil/v4/disk"
)

// Reader presents ZIP and compressed TAR entries with the same metadata.
// The caller retains ownership of the source reader.
type Reader struct {
	tar        *tar.Reader
	files      []*zip.File
	stream     io.ReadCloser
	body       io.Reader
	temporary  *os.File
	entries    int
	maxEntries int
}

func NewReader(source io.Reader, size int64, maxEntries int) (*Reader, error) {
	buffered := bufio.NewReader(source)
	header, err := buffered.Peek(4)
	if err != nil {
		return nil, err
	}
	reader := &Reader{maxEntries: maxEntries}
	if string(header) != "PK\x03\x04" && string(header) != "PK\x05\x06" {
		reader.stream, err = OpenCompressed(buffered)
		if err != nil {
			return nil, err
		}
		reader.tar = tar.NewReader(reader.stream)
		reader.body = reader.tar
		return reader, nil
	}

	random, ok := source.(io.ReaderAt)
	if !ok {
		// Remote ZIP preflights need a seekable copy to read the central directory.
		reader.temporary, err = os.CreateTemp("", "flowpanel-restore-zip-*")
		if err != nil {
			return nil, err
		}
		limit := int64(math.MaxInt64 - 1)
		if usage, usageErr := disk.Usage(os.TempDir()); usageErr == nil {
			const reserve = 512 << 20
			if usage.Free <= reserve {
				_ = reader.Close()
				return nil, errors.New("not enough storage to read ZIP archive")
			}
			limit = int64(min(uint64(limit), usage.Free-reserve))
		}
		size, err = io.Copy(reader.temporary, io.LimitReader(buffered, limit+1))
		if err != nil || size > limit {
			_ = reader.Close()
			return nil, errors.Join(errors.New("failed to stage ZIP archive within available storage"), err)
		}
		random = reader.temporary
	}
	entries, directorySize, err := InspectZipDirectory(random, size)
	if err != nil || entries > uint64(maxEntries) || directorySize > 128<<20 {
		_ = reader.Close()
		return nil, errors.New("ZIP archive directory exceeds safety limits")
	}
	archive, err := zip.NewReader(random, size)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	reader.files = archive.File
	return reader, nil
}

func (r *Reader) Read(p []byte) (int, error) {
	if r.body == nil {
		return 0, io.EOF
	}
	return r.body.Read(p)
}

func (r *Reader) Next() (*tar.Header, error) {
	if r.tar != nil {
		header, err := r.tar.Next()
		if err == io.EOF {
			// Consume the compression trailer so truncation and checksum errors surface.
			if _, err := io.Copy(io.Discard, r.stream); err != nil {
				return nil, err
			}
		}
		if err != nil {
			return nil, err
		}
		r.entries++
		if r.entries > r.maxEntries {
			return nil, errors.New("archive contains too many entries")
		}
		return header, nil
	}
	if r.stream != nil {
		_, readErr := io.Copy(io.Discard, r.stream)
		closeErr := r.stream.Close()
		r.stream, r.body = nil, nil
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
	}
	if r.entries == len(r.files) {
		return nil, io.EOF
	}
	if r.entries >= r.maxEntries {
		return nil, errors.New("archive contains too many entries")
	}
	entry := r.files[r.entries]
	r.entries++
	if entry.UncompressedSize64 > math.MaxInt64 {
		return nil, errors.New("ZIP entry is too large")
	}
	header, err := tar.FileInfoHeader(entry.FileInfo(), "")
	if err != nil {
		return nil, err
	}
	header.Name = entry.Name
	header.Size = int64(entry.UncompressedSize64)
	r.stream, err = entry.Open()
	if err != nil {
		return nil, err
	}
	r.body = r.stream
	if header.Typeflag == tar.TypeSymlink {
		if header.Size > 4096 {
			return nil, errors.New("ZIP symlink target is too long")
		}
		link, err := io.ReadAll(io.LimitReader(r.stream, 4097))
		if err != nil || int64(len(link)) != header.Size {
			return nil, errors.New("invalid ZIP symlink")
		}
		header.Linkname = string(link)
	}
	return header, nil
}

func (r *Reader) Close() error {
	var err error
	if r.stream != nil {
		err = r.stream.Close()
		r.stream = nil
	}
	if r.temporary != nil {
		err = errors.Join(err, r.temporary.Close(), os.Remove(r.temporary.Name()))
		r.temporary = nil
	}
	return err
}
