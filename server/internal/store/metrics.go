package store

import (
	"context"
	"encoding/json"
	"time"
)

// Disk is one mounted filesystem worth showing in the UI.
type Disk struct {
	Mount      string `json:"mount"`
	Device     string `json:"device"`
	TotalBytes int64  `json:"totalBytes"`
	UsedBytes  int64  `json:"usedBytes"`
}

// Sample is one point of host telemetry.
type Sample struct {
	HostID    int64     `json:"hostId"`
	TakenAt   time.Time `json:"takenAt"`
	CPUPct    float64   `json:"cpuPct"`
	MemUsed   int64     `json:"memUsed"`
	MemTotal  int64     `json:"memTotal"`
	SwapUsed  int64     `json:"swapUsed"`
	SwapTotal int64     `json:"swapTotal"`
	Load1     float64   `json:"load1"`
	UptimeS   int64     `json:"uptimeS"`
	TempC     *float64  `json:"tempC"`
	Disks     []Disk    `json:"disks"`
}

// InsertSample stores one telemetry point.
func (d *DB) InsertSample(ctx context.Context, s *Sample) error {
	disks, err := json.Marshal(s.Disks)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO metric_samples
			(host_id, taken_at, cpu_pct, mem_used, mem_total, swap_used, swap_total, load1, uptime_s, temp_c, disks)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.HostID, s.TakenAt.UTC().Format(time.RFC3339Nano), s.CPUPct, s.MemUsed, s.MemTotal,
		s.SwapUsed, s.SwapTotal, s.Load1, s.UptimeS, s.TempC, string(disks))
	return err
}

const sampleColumns = `host_id, taken_at, cpu_pct, mem_used, mem_total, swap_used,
	swap_total, load1, uptime_s, temp_c, disks`

func scanSample(row interface{ Scan(...any) error }) (*Sample, error) {
	var s Sample
	var takenAt, disks string
	if err := row.Scan(&s.HostID, &takenAt, &s.CPUPct, &s.MemUsed, &s.MemTotal,
		&s.SwapUsed, &s.SwapTotal, &s.Load1, &s.UptimeS, &s.TempC, &disks); err != nil {
		return nil, err
	}
	if t, err := parseSQLiteTime(takenAt); err == nil {
		s.TakenAt = t
	}
	s.Disks = []Disk{}
	_ = json.Unmarshal([]byte(disks), &s.Disks)
	return &s, nil
}

// LatestSamples returns the most recent sample per host, keyed by host id.
func (d *DB) LatestSamples(ctx context.Context) (map[int64]*Sample, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT `+sampleColumns+` FROM metric_samples
		WHERE id IN (SELECT MAX(id) FROM metric_samples GROUP BY host_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*Sample{}
	for rows.Next() {
		s, err := scanSample(rows)
		if err != nil {
			return nil, err
		}
		out[s.HostID] = s
	}
	return out, rows.Err()
}

// SamplesSince returns a host's samples newer than cutoff, oldest first.
func (d *DB) SamplesSince(ctx context.Context, hostID int64, cutoff time.Time) ([]*Sample, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT `+sampleColumns+` FROM metric_samples
		WHERE host_id = ? AND taken_at >= ? ORDER BY taken_at`,
		hostID, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Sample{}
	for rows.Next() {
		s, err := scanSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PruneSamples deletes telemetry older than cutoff and returns the row count.
func (d *DB) PruneSamples(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM metric_samples WHERE taken_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
