package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// walkOutput is what the probe's /proc walk prints: pid, jiffies, resident
// pages, then the name — which may itself contain spaces and brackets.
const (
	walkBefore = `1 400 2000 systemd
219 1000 60000 python3
220 50 90000 Web Content
331 10 0 kworker/0:1
480 900 5000 sshd`
	walkAfter = `1 402 2000 systemd
219 1060 61000 python3
220 55 90000 Web Content
331 30 0 kworker/0:1
480 905 5000 sshd
999 700 40000 backup.sh`
)

func TestTopConsumers(t *testing.T) {
	// 200 jiffies for the whole machine over the window, 4 KiB pages, 8 GB of
	// memory: python3 gained 60 of those jiffies, so it used 30% of the machine.
	procs := topConsumers(lines(walkBefore), lines(walkAfter), usage{
		cpuDelta: 200,
		pageSize: 4096,
		memTotal: 8 << 30,
		takenAt:  time.Now().UTC(),
	})
	if procs == nil {
		t.Fatal("topConsumers = nil, want two lists")
	}

	// Busiest first, and the tie between the two at 2.5% goes to the lower pid
	// so a machine at rest does not reshuffle its list every few seconds.
	wantCPU := []struct {
		name string
		pct  float64
	}{
		{"python3", 30},     // 60 jiffies of 200
		{"kworker/0:1", 10}, // 20 of 200
		{"Web Content", 2.5},
		{"sshd", 2.5},
		{"systemd", 1},
	}
	if len(procs.TopCPU) != len(wantCPU) {
		t.Fatalf("top cpu = %+v, want %d entries", procs.TopCPU, len(wantCPU))
	}
	for i, want := range wantCPU {
		got := procs.TopCPU[i]
		if got.Name != want.name || !closeTo(got.CPUPct, want.pct, 0.001) {
			t.Errorf("top cpu[%d] = %s at %.2f%%, want %s at %.2f%%",
				i, got.Name, got.CPUPct, want.name, want.pct)
		}
	}

	// backup.sh is only in the second walk, so there is no interval to measure
	// it over and it cannot be called busy — but its memory is real.
	for _, p := range procs.TopCPU {
		if p.Name == "backup.sh" {
			t.Error("a process that appeared inside the window was given a CPU figure")
		}
	}

	wantMem := []struct {
		name  string
		bytes int64
	}{
		{"Web Content", 90000 * 4096},
		{"python3", 61000 * 4096},
		{"backup.sh", 40000 * 4096},
		{"sshd", 5000 * 4096},
		{"systemd", 2000 * 4096},
	}
	if len(procs.TopMem) != len(wantMem) {
		t.Fatalf("top memory = %+v, want %d entries", procs.TopMem, len(wantMem))
	}
	for i, want := range wantMem {
		got := procs.TopMem[i]
		if got.Name != want.name || got.MemBytes != want.bytes {
			t.Errorf("top memory[%d] = %s at %d bytes, want %s at %d",
				i, got.Name, got.MemBytes, want.name, want.bytes)
		}
	}
	// Kernel threads hold no resident memory and would otherwise pad the list
	// with zeroes.
	for _, p := range procs.TopMem {
		if p.Name == "kworker/0:1" {
			t.Error("a process holding no memory reached the memory list")
		}
	}
	if want := 61000 * 4096 / float64(8<<30) * 100; !closeTo(procs.TopMem[1].MemPct, want, 0.001) {
		t.Errorf("python3 memPct = %.3f, want %.3f", procs.TopMem[1].MemPct, want)
	}
	// A process in both lists carries both figures, whichever list it is read
	// from.
	if procs.TopMem[1].CPUPct != procs.TopCPU[0].CPUPct {
		t.Errorf("python3 is %.2f%% in one list and %.2f%% in the other",
			procs.TopCPU[0].CPUPct, procs.TopMem[1].CPUPct)
	}
}

// Five is the point of the list: a machine with forty busy processes is not
// forty answers.
func TestTopConsumersKeepsFive(t *testing.T) {
	var before, after []string
	for pid := 1; pid <= 40; pid++ {
		before = append(before, fmt.Sprintf("%d 0 0 p%d", pid, pid))
		after = append(after, fmt.Sprintf("%d %d %d p%d", pid, pid, pid, pid))
	}
	procs := topConsumers(before, after, usage{cpuDelta: 1000, pageSize: 4096, memTotal: 1 << 30})
	if len(procs.TopCPU) != TopN || len(procs.TopMem) != TopN {
		t.Fatalf("lists = %d cpu / %d memory, want %d each", len(procs.TopCPU), len(procs.TopMem), TopN)
	}
	// The busiest first, which here is the highest pid.
	if procs.TopCPU[0].PID != 40 || procs.TopMem[0].PID != 40 {
		t.Errorf("heads = cpu %d, memory %d, want 40 in both", procs.TopCPU[0].PID, procs.TopMem[0].PID)
	}
}

// A host that says nothing — no awk, or a /proc it will not walk — leaves the
// question unanswered rather than answered with an idle machine.
func TestTopConsumersWithoutAWalk(t *testing.T) {
	if got := topConsumers(nil, nil, usage{cpuDelta: 100}); got != nil {
		t.Errorf("topConsumers with no output = %+v, want nil", got)
	}
	if got := topConsumers(lines(walkBefore), nil, usage{cpuDelta: 100}); got != nil {
		t.Errorf("topConsumers with one walk = %+v, want nil", got)
	}
}

// An idle machine reports empty lists rather than nulls: the UI maps over them.
func TestTopConsumersIdleMachine(t *testing.T) {
	procs := topConsumers(lines(walkBefore), lines(walkBefore), usage{cpuDelta: 200, memTotal: 1 << 30})
	if procs == nil || procs.TopCPU == nil {
		t.Fatalf("topConsumers = %+v, want an empty cpu list", procs)
	}
	if len(procs.TopCPU) != 0 {
		t.Errorf("top cpu on an idle machine = %+v, want nothing", procs.TopCPU)
	}
	// Memory needs no interval, so it is still there. Where the host named no
	// page size, the usual 4 KiB is assumed.
	if len(procs.TopMem) == 0 || procs.TopMem[0].MemBytes != 90000*defaultPageSize {
		t.Errorf("top memory = %+v, want it sized with the default page", procs.TopMem)
	}
}

// Garbage in a walk is skipped rather than counted as a process.
func TestParseWalkSkipsUnusableLines(t *testing.T) {
	got := parseWalk([]string{
		"1 100 200 systemd",
		"notapid 1 2 x",
		"2 nine 200 x",
		"3 100 200",
		"0 100 200 x",
		"4 100 200    ",
	})
	if len(got) != 1 {
		t.Fatalf("parseWalk = %+v, want only the usable line", got)
	}
	if got[1].name != "systemd" || got[1].jiffies != 100 || got[1].rssPages != 200 {
		t.Errorf("parsed = %+v", got[1])
	}
}

func TestPageSize(t *testing.T) {
	for _, tc := range []struct {
		lines []string
		want  int64
	}{
		{nil, defaultPageSize},
		{[]string{"16384"}, 16384},
		{[]string{"getconf: not found"}, defaultPageSize},
		{[]string{"0"}, defaultPageSize},
	} {
		if got := pageSize(tc.lines); got != tc.want {
			t.Errorf("pageSize(%v) = %d, want %d", tc.lines, got, tc.want)
		}
	}
}

func lines(out string) []string { return strings.Split(out, "\n") }
