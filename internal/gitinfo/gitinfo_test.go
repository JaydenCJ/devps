// Tests for repository and branch resolution. Everything is plain files —
// gitinfo never executes git, so neither do its tests.
package gitinfo

import (
	"os"
	"path/filepath"
	"testing"
)

// repo creates dir/.git/HEAD with the given content and returns dir.
func repo(t *testing.T, head string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvesBranchFromSymbolicRef(t *testing.T) {
	dir := repo(t, "ref: refs/heads/main\n")
	info, ok := Lookup(dir)
	if !ok {
		t.Fatal("repository not found")
	}
	if info.Branch != "main" || info.Detached || info.Root != dir {
		t.Fatalf("got %+v, want branch main at %s", info, dir)
	}
}

func TestBranchNamesWithSlashesArePreservedWhole(t *testing.T) {
	// Only the refs/heads/ prefix may be stripped; feature/checkout-flow
	// must not be truncated at its own slash.
	info, ok := Lookup(repo(t, "ref: refs/heads/feature/checkout-flow\n"))
	if !ok || info.Branch != "feature/checkout-flow" {
		t.Fatalf("got %+v, want feature/checkout-flow", info)
	}
}

func TestDetachedHeadYieldsShortHash(t *testing.T) {
	info, ok := Lookup(repo(t, "9fceb02d0ae598e95dc970b74767f19372d61af8\n"))
	if !ok {
		t.Fatal("repository not found")
	}
	if !info.Detached || info.Branch != "9fceb02" {
		t.Fatalf("got %+v, want detached at 9fceb02", info)
	}
}

func TestWalksUpFromNestedSubdirectory(t *testing.T) {
	dir := repo(t, "ref: refs/heads/main\n")
	nested := filepath.Join(dir, "src", "components", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	info, ok := Lookup(nested)
	if !ok || info.Root != dir {
		t.Fatalf("got %+v ok=%v, want root %s", info, ok, dir)
	}
}

func TestNoRepositoryReturnsFalse(t *testing.T) {
	if info, ok := Lookup(t.TempDir()); ok {
		t.Fatalf("found %+v in a bare temp dir, want none", info)
	}
}

func TestPathologicalPathsTerminate(t *testing.T) {
	// Relative paths must not resolve — procfs cwd links are always
	// absolute — and looking up "/" itself must return promptly.
	if _, ok := Lookup("relative/path"); ok {
		t.Fatal("relative path resolved to a repository")
	}
	_, _ = Lookup("/")
}

func TestGitFileWithRelativeGitdirResolvesWorktree(t *testing.T) {
	// Linked worktrees carry a .git *file* pointing at the shared metadata
	// directory, which holds a per-worktree HEAD.
	base := t.TempDir()
	meta := filepath.Join(base, "main-clone", ".git", "worktrees", "hotfix")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte("ref: refs/heads/hotfix/login\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(base, "hotfix-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "gitdir: ../main-clone/.git/worktrees/hotfix\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
	info, ok := Lookup(wt)
	if !ok || info.Branch != "hotfix/login" || info.Root != wt {
		t.Fatalf("got %+v ok=%v, want hotfix/login rooted at the worktree", info, ok)
	}
}

func TestGitFileWithAbsoluteGitdirResolves(t *testing.T) {
	base := t.TempDir()
	meta := filepath.Join(base, "meta")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+meta+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, ok := Lookup(wt)
	if !ok || info.Branch != "main" {
		t.Fatalf("got %+v ok=%v, want main", info, ok)
	}
}

func TestGitFileWithoutGitdirPrefixIsNotARepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("not a pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup(dir); ok {
		t.Fatal("a .git file without a gitdir: pointer must not count as a repository")
	}
}

func TestMissingHEADStillReportsTheRepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, ok := Lookup(dir)
	if !ok {
		t.Fatal("a repo with no HEAD is still a repo — the project column matters even when the branch is unknown")
	}
	if info.Branch != "" || info.Detached {
		t.Fatalf("got %+v, want empty branch", info)
	}
}

func TestRefOutsideHeadsIsKeptVerbatim(t *testing.T) {
	// Bisect and some CI checkouts can leave HEAD on an unusual ref; the
	// full ref is more honest than pretending it is a branch.
	info, ok := Lookup(repo(t, "ref: refs/bisect/bad\n"))
	if !ok || info.Branch != "refs/bisect/bad" {
		t.Fatalf("got %+v, want refs/bisect/bad verbatim", info)
	}
}

func TestHEADEdgeContentDegradesGracefully(t *testing.T) {
	// Trailing whitespace is trimmed; undecodable content yields an empty
	// branch rather than a lie.
	info, ok := Lookup(repo(t, "ref: refs/heads/main\n\n"))
	if !ok || info.Branch != "main" {
		t.Fatalf("got %+v, want main despite trailing whitespace", info)
	}
	info, ok = Lookup(repo(t, "zzzz not hex and not a ref\n"))
	if !ok {
		t.Fatal("repository not found")
	}
	if info.Branch != "" || info.Detached {
		t.Fatalf("got %+v, want empty branch for undecodable HEAD", info)
	}
}
