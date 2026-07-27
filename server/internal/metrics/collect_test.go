package metrics

import (
	"math"
	"testing"
)

// sampleOutput mirrors what probeScript prints on a Raspberry Pi.
const sampleOutput = `@@facts
nakedpi
6.6.31+rpt-rpi-v8
aarch64
Debian GNU/Linux 12 (bookworm)
@@sudo
yes
@@cpu1
cpu  1000 20 300 5000 100 0 10 0 0 0
@@uptime
104523.12 830192.44
@@loadavg
0.52 0.61 0.58 1/210 4821
@@meminfo
MemTotal:        8000000 kB
MemAvailable:    6000000 kB
MemFree:         5000000 kB
SwapTotal:        200000 kB
SwapFree:         150000 kB
@@df
Filesystem     1024-blocks     Used Available Capacity Mounted on
/dev/mmcblk0p2    60000000 20000000  37000000      36% /
/dev/mmcblk0p1      520000    60000    460000      12% /boot/firmware
tmpfs               400000        0    400000       0% /dev/shm
/dev/loop0           50000    50000         0     100% /snap/core
@@temp
48123
@@cpu2
cpu  1100 20 340 5400 120 0 12 0 0 0
`

func TestParseProbe(t *testing.T) {
	p, err := parseProbe(sampleOutput)
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}

	if p.Facts.Hostname != "nakedpi" {
		t.Errorf("hostname = %q, want nakedpi", p.Facts.Hostname)
	}
	if p.Facts.OS != "Debian GNU/Linux 12 (bookworm)" {
		t.Errorf("os = %q", p.Facts.OS)
	}
	if p.Facts.Arch != "aarch64" {
		t.Errorf("arch = %q, want aarch64", p.Facts.Arch)
	}
	if !p.Facts.SudoOK {
		t.Error("sudoOk = false, want true")
	}

	// busy delta 142 of 562 total jiffies.
	if want := 142.0 / 562.0 * 100; !closeTo(p.Sample.CPUPct, want, 0.01) {
		t.Errorf("cpuPct = %.3f, want %.3f", p.Sample.CPUPct, want)
	}
	if want := int64(2000000 * 1024); p.Sample.MemUsed != want {
		t.Errorf("memUsed = %d, want %d", p.Sample.MemUsed, want)
	}
	if want := int64(8000000 * 1024); p.Sample.MemTotal != want {
		t.Errorf("memTotal = %d, want %d", p.Sample.MemTotal, want)
	}
	if want := int64(50000 * 1024); p.Sample.SwapUsed != want {
		t.Errorf("swapUsed = %d, want %d", p.Sample.SwapUsed, want)
	}
	if p.Sample.Load1 != 0.52 {
		t.Errorf("load1 = %v, want 0.52", p.Sample.Load1)
	}
	if p.Sample.UptimeS != 104523 {
		t.Errorf("uptimeS = %d, want 104523", p.Sample.UptimeS)
	}
	if p.Sample.TempC == nil || !closeTo(*p.Sample.TempC, 48.123, 0.001) {
		t.Errorf("tempC = %v, want 48.123", p.Sample.TempC)
	}

	// tmpfs and loop devices are noise and must not appear.
	if len(p.Sample.Disks) != 2 {
		t.Fatalf("disks = %+v, want 2 real filesystems", p.Sample.Disks)
	}
	root := p.Sample.Disks[0]
	if root.Mount != "/" || root.Device != "/dev/mmcblk0p2" {
		t.Errorf("first disk = %+v, want / on /dev/mmcblk0p2", root)
	}
	if want := int64(20000000 * 1024); root.UsedBytes != want {
		t.Errorf("root usedBytes = %d, want %d", root.UsedBytes, want)
	}
	if p.Sample.Disks[1].Mount != "/boot/firmware" {
		t.Errorf("second disk = %+v, want /boot/firmware", p.Sample.Disks[1])
	}
}

func TestParseProbeMissingSections(t *testing.T) {
	// A minimal host: no thermal zone, no swap, sudo denied.
	out := `@@facts
box
5.15.0
x86_64
Ubuntu 22.04.4 LTS
@@sudo
no
@@cpu1
cpu  10 0 5 100 0 0 0 0 0 0
@@uptime
50.0 100.0
@@loadavg
0.00 0.01 0.05 1/100 200
@@meminfo
MemTotal:        1000000 kB
MemFree:          400000 kB
Buffers:           50000 kB
Cached:           150000 kB
@@df
Filesystem     1024-blocks     Used Available Capacity Mounted on
/dev/sda1         10000000  4000000   6000000      40% /
@@temp
@@cpu2
cpu  10 0 5 100 0 0 0 0 0 0
`
	p, err := parseProbe(out)
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if p.Facts.SudoOK {
		t.Error("sudoOk = true, want false")
	}
	if p.Sample.TempC != nil {
		t.Errorf("tempC = %v, want nil when no thermal zone", *p.Sample.TempC)
	}
	// No MemAvailable: fall back to free + buffers + cached.
	if want := int64(400000 * 1024); p.Sample.MemUsed != want {
		t.Errorf("memUsed = %d, want %d", p.Sample.MemUsed, want)
	}
	if p.Sample.SwapTotal != 0 || p.Sample.SwapUsed != 0 {
		t.Errorf("swap = %d/%d, want 0/0", p.Sample.SwapUsed, p.Sample.SwapTotal)
	}
	// Identical /proc/stat reads mean no elapsed jiffies, not 100% busy.
	if p.Sample.CPUPct != 0 {
		t.Errorf("cpuPct = %v, want 0 for a zero-length interval", p.Sample.CPUPct)
	}
}

func TestParseProbeGarbage(t *testing.T) {
	if _, err := parseProbe("bash: /proc/stat: Permission denied\n"); err == nil {
		t.Error("expected an error when no sections are present")
	}
}

func closeTo(got, want, tol float64) bool { return math.Abs(got-want) <= tol }
