package pgtest

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Supported range is PostgreSQL 15 through 19 (decision D11). 19 runs its
// release candidate until it is released.
// Minors are chosen above the reverted-ABI releases documented in R2.
var images = map[int]string{
	15: "postgres:15.19-alpine",
	16: "postgres:16.15-alpine",
	17: "postgres:17.11-alpine",
	18: "postgres:18.6-alpine",
	19: "postgres:19rc1-alpine",
}

// Versions returns the majors to exercise. PGLENS_PG_VERSIONS overrides it;
// the default is the PR matrix (the oldest and the newest supported), and
// CI sets the full range for the nightly run. A major without an image is a
// panic, not a skip: a run that quietly drops it reports green while testing
// a different matrix than the one that was asked for.
func Versions() []int {
	if v := os.Getenv("PGLENS_PG_VERSIONS"); v != "" {
		var out []int
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				panic(fmt.Sprintf("PGLENS_PG_VERSIONS: %q is not a PostgreSQL major (supported: %v)", s, supportedMajors()))
			}
			if _, ok := images[n]; !ok {
				panic(fmt.Sprintf("PGLENS_PG_VERSIONS: PostgreSQL %d has no image (supported: %v)", n, supportedMajors()))
			}
			out = append(out, n)
		}
		if len(out) == 0 {
			panic(fmt.Sprintf("PGLENS_PG_VERSIONS=%q names no PostgreSQL major (supported: %v)", v, supportedMajors()))
		}
		return out
	}
	return []int{15, 18}
}

// ImageFor returns the docker image for a major version. An unknown major
// panics for the same reason Versions does: an empty image name would only
// fail later, inside testcontainers, with a message that does not name it.
func ImageFor(major int) string {
	img, ok := images[major]
	if !ok {
		panic(fmt.Sprintf("PostgreSQL %d has no image (supported: %v)", major, supportedMajors()))
	}
	return img
}

// supportedMajors lists the majors that have an image, in order, for the
// panic messages above.
func supportedMajors() []int {
	out := make([]int, 0, len(images))
	for n := range images {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
