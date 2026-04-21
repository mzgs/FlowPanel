package systemmonitor

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbutil "flowpanel/internal/db"
	"flowpanel/internal/systemstatus"
)

type Store struct {
	db *sql.DB
}

type Sample struct {
	SampledAt        time.Time `json:"sampled_at"`
	CPUUsagePercent  *float64  `json:"cpu_usage_percent,omitempty"`
	DiskFreeBytes    *uint64   `json:"disk_free_bytes,omitempty"`
	DiskReadBytes    *uint64   `json:"disk_read_bytes,omitempty"`
	DiskReadCount    *uint64   `json:"disk_read_count,omitempty"`
	DiskTotalBytes   *uint64   `json:"disk_total_bytes,omitempty"`
	DiskUsedBytes    *uint64   `json:"disk_used_bytes,omitempty"`
	DiskWriteBytes   *uint64   `json:"disk_write_bytes,omitempty"`
	DiskWriteCount   *uint64   `json:"disk_write_count,omitempty"`
	MemoryTotalBytes *uint64   `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes  *uint64   `json:"memory_used_bytes,omitempty"`
	NetworkRecvBytes *uint64   `json:"network_receive_bytes,omitempty"`
	NetworkSentBytes *uint64   `json:"network_transmit_bytes,omitempty"`
}

func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}

	return &Store{db: db}
}

func NewSample(sampledAt time.Time, status systemstatus.Status) Sample {
	return Sample{
		SampledAt:        sampledAt.UTC(),
		CPUUsagePercent:  status.CPUUsagePercent,
		DiskFreeBytes:    status.DiskFreeBytes,
		DiskReadBytes:    status.DiskReadBytes,
		DiskReadCount:    status.DiskReadCount,
		DiskTotalBytes:   status.DiskTotalBytes,
		DiskUsedBytes:    status.DiskUsedBytes,
		DiskWriteBytes:   status.DiskWriteBytes,
		DiskWriteCount:   status.DiskWriteCount,
		MemoryTotalBytes: status.MemoryTotalBytes,
		MemoryUsedBytes:  status.MemoryUsedBytes,
		NetworkRecvBytes: status.NetworkRecvBytes,
		NetworkSentBytes: status.NetworkSentBytes,
	}
}

func (s *Store) Ensure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}

	const statement = `
CREATE TABLE IF NOT EXISTS system_monitor_samples (
    sampled_at INTEGER PRIMARY KEY,
    cpu_usage_percent REAL,
    disk_free_bytes INTEGER,
    disk_read_bytes INTEGER,
    disk_read_count INTEGER,
    disk_total_bytes INTEGER,
    disk_used_bytes INTEGER,
    disk_write_bytes INTEGER,
    disk_write_count INTEGER,
    memory_total_bytes INTEGER,
    memory_used_bytes INTEGER,
    network_receive_bytes INTEGER,
    network_transmit_bytes INTEGER
);

CREATE INDEX IF NOT EXISTS idx_system_monitor_samples_sampled_at
ON system_monitor_samples (sampled_at DESC);
`

	if err := dbutil.ExecStatements(ctx, s.db, dbutil.Statement{
		SQL:          statement,
		ErrorContext: "ensure system monitor samples table",
	}); err != nil {
		return err
	}

	if err := ensureColumn(ctx, s.db, "system_monitor_samples", "memory_total_bytes", "INTEGER"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, s.db, "system_monitor_samples", "disk_read_bytes", "INTEGER"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, s.db, "system_monitor_samples", "disk_read_count", "INTEGER"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, s.db, "system_monitor_samples", "disk_write_bytes", "INTEGER"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, s.db, "system_monitor_samples", "disk_write_count", "INTEGER"); err != nil {
		return err
	}

	return ensureColumn(ctx, s.db, "system_monitor_samples", "memory_used_bytes", "INTEGER")
}

func (s *Store) Insert(ctx context.Context, sample Sample) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT OR REPLACE INTO system_monitor_samples (
    sampled_at,
    cpu_usage_percent,
    disk_free_bytes,
    disk_read_bytes,
    disk_read_count,
    disk_total_bytes,
    disk_used_bytes,
    disk_write_bytes,
    disk_write_count,
    memory_total_bytes,
    memory_used_bytes,
    network_receive_bytes,
    network_transmit_bytes
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		sample.SampledAt.UTC().UnixNano(),
		sample.CPUUsagePercent,
		sample.DiskFreeBytes,
		sample.DiskReadBytes,
		sample.DiskReadCount,
		sample.DiskTotalBytes,
		sample.DiskUsedBytes,
		sample.DiskWriteBytes,
		sample.DiskWriteCount,
		sample.MemoryTotalBytes,
		sample.MemoryUsedBytes,
		sample.NetworkRecvBytes,
		sample.NetworkSentBytes,
	)
	if err != nil {
		return fmt.Errorf("insert system monitor sample: %w", err)
	}

	return nil
}

func (s *Store) ListSince(ctx context.Context, since time.Time) ([]Sample, error) {
	if s == nil || s.db == nil {
		return []Sample{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT sampled_at, cpu_usage_percent, disk_free_bytes, disk_read_bytes, disk_read_count, disk_total_bytes, disk_used_bytes, disk_write_bytes, disk_write_count, memory_total_bytes, memory_used_bytes, network_receive_bytes, network_transmit_bytes
FROM system_monitor_samples
WHERE sampled_at >= ?
ORDER BY sampled_at ASC
`, since.UTC().UnixNano())
	if err != nil {
		return nil, fmt.Errorf("list system monitor samples: %w", err)
	}
	defer rows.Close()

	samples := make([]Sample, 0)
	for rows.Next() {
		var (
			sample            Sample
			sampledAtUnixNano int64
			cpuUsagePercent   sql.NullFloat64
			diskFreeBytes     sql.NullInt64
			diskReadBytes     sql.NullInt64
			diskReadCount     sql.NullInt64
			diskTotalBytes    sql.NullInt64
			diskUsedBytes     sql.NullInt64
			diskWriteBytes    sql.NullInt64
			diskWriteCount    sql.NullInt64
			memoryTotalBytes  sql.NullInt64
			memoryUsedBytes   sql.NullInt64
			networkRecvBytes  sql.NullInt64
			networkSentBytes  sql.NullInt64
		)

		if err := rows.Scan(
			&sampledAtUnixNano,
			&cpuUsagePercent,
			&diskFreeBytes,
			&diskReadBytes,
			&diskReadCount,
			&diskTotalBytes,
			&diskUsedBytes,
			&diskWriteBytes,
			&diskWriteCount,
			&memoryTotalBytes,
			&memoryUsedBytes,
			&networkRecvBytes,
			&networkSentBytes,
		); err != nil {
			return nil, fmt.Errorf("scan system monitor sample row: %w", err)
		}

		sample.SampledAt = time.Unix(0, sampledAtUnixNano).UTC()
		sample.CPUUsagePercent = nullFloat64Ptr(cpuUsagePercent)
		sample.DiskFreeBytes = nullUint64Ptr(diskFreeBytes)
		sample.DiskReadBytes = nullUint64Ptr(diskReadBytes)
		sample.DiskReadCount = nullUint64Ptr(diskReadCount)
		sample.DiskTotalBytes = nullUint64Ptr(diskTotalBytes)
		sample.DiskUsedBytes = nullUint64Ptr(diskUsedBytes)
		sample.DiskWriteBytes = nullUint64Ptr(diskWriteBytes)
		sample.DiskWriteCount = nullUint64Ptr(diskWriteCount)
		sample.MemoryTotalBytes = nullUint64Ptr(memoryTotalBytes)
		sample.MemoryUsedBytes = nullUint64Ptr(memoryUsedBytes)
		sample.NetworkRecvBytes = nullUint64Ptr(networkRecvBytes)
		sample.NetworkSentBytes = nullUint64Ptr(networkSentBytes)
		samples = append(samples, sample)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate system monitor samples: %w", err)
	}

	return samples, nil
}

func (s *Store) DeleteBefore(ctx context.Context, cutoff time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM system_monitor_samples WHERE sampled_at < ?`, cutoff.UTC().UnixNano()); err != nil {
		return fmt.Errorf("delete expired system monitor samples: %w", err)
	}

	return nil
}

func nullFloat64Ptr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}

	result := value.Float64
	return &result
}

func nullUint64Ptr(value sql.NullInt64) *uint64 {
	if !value.Valid || value.Int64 < 0 {
		return nil
	}

	result := uint64(value.Int64)
	return &result
}

func ensureColumn(ctx context.Context, db *sql.DB, table string, column string, definition string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			kind       string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)

		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan %s column info: %w", table, err)
		}

		if name == column {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s column info: %w", table, err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column, err)
	}

	return nil
}
