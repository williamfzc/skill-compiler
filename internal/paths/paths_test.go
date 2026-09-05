// paths_test.go -- foundation helpers, notably the display path shortening.
package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShorten(t *testing.T) {
	realHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)

	if got := Shorten(filepath.Join(realHome, "a", "b")); got != "~/a/b" {
		t.Fatalf("home prefix collapses to ~, got %q", got)
	}

	// The home string appearing mid-path with no separator boundary must not
	// collapse (naive substring replace turned "/private/tmp/.../<home>..."
	// into "/private~/...").
	decoy := filepath.Join(filepath.Dir(realHome), filepath.Base(realHome)+"x", "y")
	if got := Shorten(decoy); got != decoy {
		t.Fatalf("non-boundary home substring must stay intact, got %q", got)
	}

	// $HOME given through a symlink: realpathed paths below it still shorten.
	link := filepath.Join(t.TempDir(), "homelink")
	if err := os.Symlink(realHome, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", link)
	if got := Shorten(filepath.Join(realHome, "c")); got != "~/c" {
		t.Fatalf("realpathed path under symlinked HOME should shorten, got %q", got)
	}
}
