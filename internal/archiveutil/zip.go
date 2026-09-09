package archiveutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

func InspectZipDirectory(reader io.ReaderAt, size int64) (uint64, uint64, error) {
	tailSize := min(size, int64(65_557))
	if tailSize < 22 {
		return 0, 0, errors.New("ZIP end record is missing")
	}
	tail := make([]byte, tailSize)
	if _, err := reader.ReadAt(tail, size-tailSize); err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	endIndex := bytes.LastIndex(tail, []byte{'P', 'K', 5, 6})
	if endIndex < 0 || len(tail)-endIndex < 22 {
		return 0, 0, errors.New("ZIP end record is missing")
	}
	commentSize := int(binary.LittleEndian.Uint16(tail[endIndex+20 : endIndex+22]))
	if endIndex+22+commentSize != len(tail) {
		return 0, 0, errors.New("ZIP end record is invalid")
	}
	entries := uint64(binary.LittleEndian.Uint16(tail[endIndex+10 : endIndex+12]))
	directorySize := uint64(binary.LittleEndian.Uint32(tail[endIndex+12 : endIndex+16]))
	if entries != 0xffff && directorySize != 0xffffffff {
		return entries, directorySize, nil
	}
	locatorIndex := bytes.LastIndex(tail[:endIndex], []byte{'P', 'K', 6, 7})
	if locatorIndex < 0 || len(tail)-locatorIndex < 20 {
		return 0, 0, errors.New("ZIP64 locator is missing")
	}
	record := make([]byte, 56)
	if _, err := reader.ReadAt(record, int64(binary.LittleEndian.Uint64(tail[locatorIndex+8:locatorIndex+16]))); err != nil || !bytes.Equal(record[:4], []byte{'P', 'K', 6, 6}) {
		return 0, 0, errors.New("ZIP64 end record is invalid")
	}
	return binary.LittleEndian.Uint64(record[32:40]), binary.LittleEndian.Uint64(record[40:48]), nil
}
