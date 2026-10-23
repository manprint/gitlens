package pgtest

import (
	"reflect"
	"strings"
	"testing"
)

// panicMessage runs fn and returns what it panicked with, or "" if it did not.
func panicMessage(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg, _ = r.(string)
		}
	}()
	fn()
	return ""
}

func TestVersions_DefaultsToTheOldestAndNewestWhenUnset(t *testing.T) {
	t.Setenv("PGLENS_PG_VERSIONS", "")
	if got := Versions(); !reflect.DeepEqual(got, []int{15, 18}) {
		t.Fatalf("Versions() = %v, want [15 18]", got)
	}
}

func TestVersions_ParsesEveryMajorInTheList(t *testing.T) {
	t.Setenv("PGLENS_PG_VERSIONS", "15, 19")
	if got := Versions(); !reflect.DeepEqual(got, []int{15, 19}) {
		t.Fatalf("Versions() = %v, want [15 19]", got)
	}
}

// The regression: 19 has an image, so the call must return it. Before the
// fix an unknown major was dropped and the default [15 18] came back, which
// is how PGLENS_PG_VERSIONS=19 once ran 15 and 18 and reported green.
func TestVersions_PanicsOnAMajorWithoutAnImage(t *testing.T) {
	t.Setenv("PGLENS_PG_VERSIONS", "15,20")
	msg := panicMessage(func() { Versions() })
	if !strings.Contains(msg, "PostgreSQL 20 has no image") || !strings.Contains(msg, "[15 16 17 18 19]") {
		t.Fatalf("Versions() with 15,20 panicked with %q, want a message naming 20 and the supported majors", msg)
	}
}

func TestVersions_PanicsOnAValueThatIsNotAMajor(t *testing.T) {
	for _, v := range []string{"abc", ","} {
		t.Setenv("PGLENS_PG_VERSIONS", v)
		if msg := panicMessage(func() { Versions() }); msg == "" {
			t.Errorf("Versions() with %q returned instead of failing", v)
		}
	}
}

func TestImageFor_ReturnsTheImageForASupportedMajor(t *testing.T) {
	if got := ImageFor(19); got != "postgres:19rc1-alpine" {
		t.Fatalf("ImageFor(19) = %q, want postgres:19rc1-alpine", got)
	}
}

func TestImageFor_PanicsOnAMajorWithoutAnImage(t *testing.T) {
	msg := panicMessage(func() { ImageFor(20) })
	if !strings.Contains(msg, "PostgreSQL 20 has no image") {
		t.Fatalf("ImageFor(20) panicked with %q, want a message naming 20", msg)
	}
}
