// Package gitinfo resolves the git repository and checked-out branch for a
// working directory by reading .git metadata directly — no git binary is
// executed, so lookups are fast, deterministic, and work on hosts without
// git installed.
package gitinfo

import (
	"os"
	"path/filepath"
	"strings"
)

// Info describes the repository that owns a directory.
type Info struct {
	Root     string // top-level working directory (the one containing .git)
	Branch   string // branch name; a short commit hash when detached; "" if unreadable
	Detached bool   // true when HEAD points at a commit instead of a branch
}

// maxDepth caps the upward walk so a pathological symlink loop presented
// as a very deep path can never stall a scan.
const maxDepth = 64

// Lookup walks from dir toward the filesystem root looking for a .git
// entry, then resolves HEAD. The second return is false when dir is not
// inside a git repository (or is not an absolute path).
func Lookup(dir string) (Info, bool) {
	d := filepath.Clean(dir)
	if d == "" || !filepath.IsAbs(d) {
		return Info{}, false
	}
	for i := 0; i < maxDepth; i++ {
		gitPath := filepath.Join(d, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			gitDir := gitPath
			if !fi.IsDir() {
				// Worktrees and submodules use a .git *file* holding
				// "gitdir: <path>" that points at the real metadata dir.
				gitDir = resolveGitFile(gitPath, d)
			}
			if gitDir != "" {
				info := Info{Root: d}
				info.Branch, info.Detached = readHead(gitDir)
				return info, true
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return Info{}, false
}

// resolveGitFile reads a "gitdir:" pointer file; relative targets are
// resolved against the directory containing the .git file.
func resolveGitFile(path, containing string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(containing, target)
	}
	return filepath.Clean(target)
}

// readHead decodes HEAD: either a symbolic ref ("ref: refs/heads/main") or
// a bare commit hash for a detached HEAD, shortened to 7 characters the
// way git itself abbreviates.
func readHead(gitDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(raw))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		if branch, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return branch, false
		}
		return ref, false // unusual, e.g. a ref outside refs/heads
	}
	if isHex(head) && len(head) >= 7 {
		return head[:7], true
	}
	return "", false
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return s != ""
}
