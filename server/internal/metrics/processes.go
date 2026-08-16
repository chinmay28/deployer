package metrics

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// What is using the machine?
//
// The CPU meter says whether a host is busy. It never says busy with what,
// which is the next thing anyone asks and the thing they opened a terminal for
// until now. So the probe brings back the few processes that account for the
// reading: the biggest consumers of CPU, and the biggest consumers of memory.
//
// The CPU figure has to be earned rather than read. `ps` reports a process's
// average since it started, which on a machine that has been up for a month
// says almost nothing about what is happening now — a backup that pegged a core
// at 3am still looks like the busiest thing on an idle afternoon. So each
// process is measured the way the host's own CPU line is: /proc is walked at
// both ends of the same second the probe already sleeps for, and what is
// reported is the jiffies that process gained in between, over the jiffies the
// whole machine gained. That makes a process's percentage a share of the whole
// machine, the same scale as the CPU meter above it, so four cores fully busy
// is 100% between them rather than 400%.
//
// A process therefore needs to exist at both ends of the second to have a CPU
// figure. One that started inside the window has nothing to be measured
// against, and counting its whole lifetime instead would let a stale reading
// from a reused pid claim the top of the list. Memory needs no interval, so the
// memory list is simply what was resident at the end.

// TopN is how long each list is. Five is what fits on a phone above the fold,
// and past the fifth the answer is "nothing in particular".
const TopN = 5

// defaultPageSize is what an RSS in pages is worth where the host would not say.
// Every mainstream arm64 and amd64 Linux uses 4 KiB pages.
const defaultPageSize = 4096

// Process is one process as it appeared during the probe's window.
type Process struct {
	PID int `json:"pid"`
	// Name is comm, which the kernel truncates to 15 characters.
	Name string `json:"name"`
	// CPUPct is the share of the whole machine this process used during the
	// window, so it is comparable with the host's own CPU figure.
	CPUPct float64 `json:"cpuPct"`
	// MemBytes is resident memory, and MemPct the same as a share of the total
	// the host reported.
	MemBytes int64   `json:"memBytes"`
	MemPct   float64 `json:"memPct"`
}

// Processes is what the machine was busy with at one moment: the top consumers
// of each resource, which are usually but not always the same processes.
type Processes struct {
	TakenAt time.Time `json:"takenAt"`
	TopCPU  []Process `json:"topCpu"`
	TopMem  []Process `json:"topMem"`
}

// usage carries what turns raw counters into percentages.
type usage struct {
	// cpuDelta is the jiffies the whole machine gained over the window.
	cpuDelta int64
	pageSize int64
	memTotal int64
	takenAt  time.Time
}

// procStat is one line of the probe's /proc walk: "pid jiffies rss-pages name".
type procStat struct {
	jiffies  int64
	rssPages int64
	name     string
}

// topConsumers joins the two walks and reduces them to the two lists. It
// returns nil where the host gave nothing to work with, which reads in the UI
// as a host that does not answer this question rather than as an idle one.
func topConsumers(before, after []string, u usage) *Processes {
	first, second := parseWalk(before), parseWalk(after)
	if len(second) == 0 {
		return nil
	}
	if u.pageSize <= 0 {
		u.pageSize = defaultPageSize
	}

	byCPU := make([]Process, 0, len(second))
	byMem := make([]Process, 0, len(second))
	for pid, now := range second {
		p := Process{PID: pid, Name: now.name, MemBytes: now.rssPages * u.pageSize}
		if u.memTotal > 0 {
			p.MemPct = clamp(float64(p.MemBytes)/float64(u.memTotal)*100, 0, 100)
		}
		// No entry in the first walk means no interval to measure over.
		if was, ran := first[pid]; ran && u.cpuDelta > 0 {
			if spent := now.jiffies - was.jiffies; spent > 0 {
				p.CPUPct = clamp(float64(spent)/float64(u.cpuDelta)*100, 0, 100)
				byCPU = append(byCPU, p)
			}
		}
		if p.MemBytes > 0 {
			byMem = append(byMem, p)
		}
	}

	return &Processes{
		TakenAt: u.takenAt,
		TopCPU:  top(byCPU, func(p Process) float64 { return p.CPUPct }),
		TopMem:  top(byMem, func(p Process) float64 { return float64(p.MemBytes) }),
	}
}

// top sorts by one measure, biggest first, and keeps the head of the list. Ties
// are broken by pid so a machine at rest does not reshuffle its list every few
// seconds for no reason.
func top(list []Process, by func(Process) float64) []Process {
	sort.Slice(list, func(i, j int) bool {
		if a, b := by(list[i]), by(list[j]); a != b {
			return a > b
		}
		return list[i].PID < list[j].PID
	})
	if len(list) > TopN {
		list = list[:TopN]
	}
	// Never null: the UI maps over these.
	if list == nil {
		return []Process{}
	}
	return list
}

// parseWalk reads the probe's per-process lines. A name may contain spaces —
// "Web Content" is a real one — so it is whatever is left after the three
// numbers, not a field.
func parseWalk(lines []string) map[int]procStat {
	out := make(map[int]procStat, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, " ", 4)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		jiffies, err1 := strconv.ParseInt(fields[1], 10, 64)
		rss, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		name := strings.TrimSpace(fields[3])
		if name == "" {
			continue
		}
		out[pid] = procStat{jiffies: jiffies, rssPages: max(rss, 0), name: name}
	}
	return out
}

func pageSize(lines []string) int64 {
	if len(lines) == 0 {
		return defaultPageSize
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil || size <= 0 {
		return defaultPageSize
	}
	return size
}
