package index

// IndexExtraTree must follow symlinks: Nix profiles and Homebrew opt/ paths are
// symlink FORESTS (root, package dirs, and files are all links into a store).
// Regression: the plain workspace walk (filepath.WalkDir) follows none of them
// and silently indexed almost nothing from a Nix profile.

import (
	"os"
	"path/filepath"
	"testing"
)

// buildSymlinkForest mirrors a real Nix profile:
//
//	store/pkgA-lib/a.tcl      real source
//	store/b.tcl               real source
//	store/mod-1.0.tm          real source (Tcl module)
//	env/lib/pkgA -> store/pkgA-lib      (dir symlink, buildEnv style)
//	env/lib/b.tcl -> store/b.tcl        (file symlink)
//	env/lib/mod-1.0.tm -> store/mod-1.0.tm
//	profile-1-link -> env               (generation link)
//	profile -> profile-1-link           (profile link)
func buildSymlinkForest(t *testing.T) (profile string) {
	t.Helper()
	base := t.TempDir()
	store := filepath.Join(base, "store")
	if err := os.MkdirAll(filepath.Join(store, "pkgA-lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, store, "pkgA-lib/a.tcl", "proc pkga_helper {} {}\n")
	writeFile(t, store, "b.tcl", "proc pkgb_helper {} {}\n")
	writeFile(t, store, "mod-1.0.tm", "proc mod_helper {} {}\n")

	env := filepath.Join(base, "env")
	if err := os.MkdirAll(filepath.Join(env, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(env, "lib", "pkgA"):       filepath.Join(store, "pkgA-lib"),
		filepath.Join(env, "lib", "b.tcl"):      filepath.Join(store, "b.tcl"),
		filepath.Join(env, "lib", "mod-1.0.tm"): filepath.Join(store, "mod-1.0.tm"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	gen := filepath.Join(base, "profile-1-link")
	if err := os.Symlink(env, gen); err != nil {
		t.Fatal(err)
	}
	profile = filepath.Join(base, "profile")
	if err := os.Symlink(gen, profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestIndexExtraTreeFollowsSymlinkForest(t *testing.T) {
	profile := buildSymlinkForest(t)
	ix := New()
	n, err := ix.IndexExtraTree(filepath.Join(profile, "lib"), map[string]bool{})
	if err != nil {
		t.Fatalf("IndexExtraTree error: %v", err)
	}
	if n != 3 {
		t.Fatalf("indexed %d files, want 3 (dir link, file link, .tm link)", n)
	}
	for _, name := range []string{"::pkga_helper", "::pkgb_helper", "::mod_helper"} {
		if locs := ix.Lookup(name); len(locs) != 1 {
			t.Errorf("%s not indexed exactly once through the symlink forest: %#v", name, locs)
		}
	}
	// Files keep the traversal (profile-relative) path, not the store path, so
	// goto-def targets survive store churn across profile upgrades.
	locs := ix.Lookup("::pkga_helper")
	if len(locs) == 1 && !filepath.HasPrefix(locs[0].File, profile) {
		t.Errorf("indexed under %q, want a profile-relative path under %q", locs[0].File, profile)
	}
}

// A symlink cycle (dir link pointing back at an ancestor) must terminate and
// not duplicate anything.
func TestIndexExtraTreeCycleSafe(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "sub/x.tcl", "proc cyc_helper {} {}\n")
	if err := os.Symlink(root, filepath.Join(root, "sub", "loop")); err != nil {
		t.Fatal(err)
	}
	ix := New()
	n, err := ix.IndexExtraTree(root, map[string]bool{})
	if err != nil {
		t.Fatalf("IndexExtraTree error: %v", err)
	}
	if n != 1 {
		t.Fatalf("indexed %d files, want exactly 1 despite the cycle", n)
	}
	if locs := ix.Lookup("::cyc_helper"); len(locs) != 1 {
		t.Fatalf("cyc_helper indexed %d times, want 1: %#v", len(locs), locs)
	}
}

// Two links to the same store dir index its files once (dedup by resolved
// identity), and a plain real directory still works (no symlinks required).
func TestIndexExtraTreeDupLinksAndPlainDirs(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, "store", "pkg")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "store"), "pkg/p.tcl", "proc dup_helper {} {}\n")
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(root, "first")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(root, "second")); err != nil {
		t.Fatal(err)
	}
	// Two FILE links to the same store file must also collapse to one location.
	writeFile(t, base, "store/f.tcl", "proc dupfile_helper {} {}\n")
	if err := os.Symlink(filepath.Join(base, "store", "f.tcl"), filepath.Join(root, "x.tcl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "store", "f.tcl"), filepath.Join(root, "y.tcl")); err != nil {
		t.Fatal(err)
	}
	ix := New()
	if _, err := ix.IndexExtraTree(root, map[string]bool{}); err != nil {
		t.Fatalf("IndexExtraTree error: %v", err)
	}
	if locs := ix.Lookup("::dup_helper"); len(locs) != 1 {
		t.Fatalf("dup_helper indexed %d times through two links, want 1: %#v", len(locs), locs)
	}
	if locs := ix.Lookup("::dupfile_helper"); len(locs) != 1 {
		t.Fatalf("dupfile_helper indexed %d times through two file links, want 1: %#v", len(locs), locs)
	}

	plain := t.TempDir()
	writeFile(t, plain, "n.tcl", "proc plain_helper {} {}\n")
	ix2 := New()
	n, err := ix2.IndexExtraTree(plain, map[string]bool{})
	if err != nil || n != 1 {
		t.Fatalf("plain dir: n=%d err=%v, want 1/nil", n, err)
	}
}

// Two CONFIGURED roots whose symlinks reach the same store file (a profile and
// its subdir, or two generation links) must still collapse to one location --
// the seen map is shared across IndexExtraTree calls, as indexExtraPaths does.
func TestIndexExtraTreeOverlappingRoots(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, store, "helper.tcl", "proc overlap_helper {} {}\n")
	rootA := filepath.Join(base, "rootA")
	rootB := filepath.Join(base, "rootB")
	for _, r := range []string{rootA, rootB} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(store, filepath.Join(r, "lib")); err != nil {
			t.Fatal(err)
		}
	}
	ix := New()
	seen := map[string]bool{}
	for _, r := range []string{rootA, rootB} {
		if _, err := ix.IndexExtraTree(r, seen); err != nil {
			t.Fatalf("IndexExtraTree(%s) error: %v", r, err)
		}
	}
	if locs := ix.Lookup("::overlap_helper"); len(locs) != 1 {
		t.Fatalf("overlap_helper indexed %d times across two overlapping roots, want 1: %#v", len(locs), locs)
	}
}
