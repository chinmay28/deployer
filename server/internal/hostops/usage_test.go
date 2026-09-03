package hostops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func usageOf(t *testing.T, dir string, extraPath ...string) (*Usage, int) {
	t.Helper()
	out, code := runScript(t, asUser(usageScript, dir), "", extraPath...)
	if code != 0 {
		return nil, code
	}
	u, err := parseUsage(out, dir)
	if err != nil {
		t.Fatalf("parseUsage: %v\n%s", err, out)
	}
	return u, 0
}

// A tree with files at several depths, a nested directory, and links: the
// counts must reach all the way down, count a link as a link rather than as
// what it points at, and leave the directory itself out of the folder count.
func TestUsageScript(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "empty"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "top.txt"), []byte("top\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "a", "mid.txt"), []byte("mid\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "a", "b", "c", "deep.bin"), make([]byte, 64<<10), 0o644))
	must(os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "to-a")))
	must(os.Symlink("/nowhere-at-all", filepath.Join(dir, "dangling")))

	u, code := usageOf(t, dir)
	if code != 0 {
		t.Fatalf("usage exited %d", code)
	}
	if u.Path != dir {
		t.Errorf("path = %q, want %q", u.Path, dir)
	}
	// top.txt, mid.txt, deep.bin, and the two symlinks as themselves.
	if u.Files != 5 {
		t.Errorf("files = %d, want 5", u.Files)
	}
	// a, a/b, a/b/c, empty — not the root, and not the link to a.
	if u.Dirs != 4 {
		t.Errorf("dirs = %d, want 4", u.Dirs)
	}
	if u.Bytes < 64<<10 {
		t.Errorf("bytes = %d, want at least the 64 KB file", u.Bytes)
	}
	if u.Bytes%1024 != 0 {
		t.Errorf("bytes = %d, want a whole number of kilobytes from du -sk", u.Bytes)
	}
	if u.Unreadable != 0 {
		t.Errorf("unreadable = %d, want 0 on a tree that is all readable", u.Unreadable)
	}
	if u.AsUser == "" {
		t.Error("asUser is empty")
	}
}

// A symlink to a directory is measured as that directory, and reports where it
// led rather than the link's own name.
func TestUsageScriptFollowsALinkToADirectory(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	u, code := usageOf(t, link)
	if code != 0 {
		t.Fatalf("usage exited %d", code)
	}
	if u.Path != real {
		t.Errorf("path = %q, want the resolved %q", u.Path, real)
	}
	if u.Files != 1 || u.Dirs != 0 {
		t.Errorf("files, dirs = %d, %d; want 1, 0", u.Files, u.Dirs)
	}
}

// An empty directory is a directory with nothing in it, not an error.
func TestUsageScriptOnAnEmptyDirectory(t *testing.T) {
	u, code := usageOf(t, t.TempDir())
	if code != 0 {
		t.Fatalf("usage exited %d", code)
	}
	if u.Files != 0 || u.Dirs != 0 {
		t.Errorf("files, dirs = %d, %d; want 0, 0", u.Files, u.Dirs)
	}
	if u.Bytes <= 0 {
		t.Errorf("bytes = %d, want the directory's own block", u.Bytes)
	}
}

// Somewhere the walk cannot enter is reported as a caveat on the numbers, not
// as a failure of the whole request.
func TestUsageScriptCountsWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything, so nothing is unreadable")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seen"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	u, code := usageOf(t, dir)
	if code != 0 {
		t.Fatalf("usage exited %d, want the readable part measured", code)
	}
	if u.Files != 1 {
		t.Errorf("files = %d, want the 1 that could be seen", u.Files)
	}
	if u.Dirs != 1 {
		t.Errorf("dirs = %d, want the locked directory itself counted", u.Dirs)
	}
	if u.Unreadable != 1 {
		t.Errorf("unreadable = %d, want the one locked directory, counted once", u.Unreadable)
	}
}

func TestUsageScriptRefusesWhatIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := usageOf(t, file); code != 3 {
		t.Errorf("a file exited %d, want 3", code)
	}
	if _, code := usageOf(t, filepath.Join(dir, "missing")); code != 2 {
		t.Errorf("a missing path exited %d, want 2", code)
	}
}

// Output that is not the script's — a host that printed a login banner and
// nothing else, say — is an error, not a directory with nothing in it.
func TestParseUsageRejectsNonsense(t *testing.T) {
	for _, out := range []string{"", "Welcome to the pi\n", "@@files\n\n@@dirs\n1\n@@kb\n1\n@@unreadable\n0\n", "@@files\nlots\n"} {
		if _, err := parseUsage(out, "/x"); err == nil {
			t.Errorf("parseUsage(%q) = nil error, want a refusal", out)
		}
	}
	u, err := parseUsage("@@path\n/x\n@@user\nroot\n@@files\n12\n@@dirs\n3\n@@kb\n40\n@@unreadable\n2\n", "/asked")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%+v", &Usage{Path: "/x", AsUser: "root", Files: 12, Dirs: 3, Bytes: 40 * 1024, Unreadable: 2})
	if got := fmt.Sprintf("%+v", u); got != want {
		t.Errorf("parsed %s, want %s", got, want)
	}
}
