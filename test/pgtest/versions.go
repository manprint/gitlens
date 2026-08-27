package pgtest

import (
	"os"
	"strconv"
	"strings"
)

// Supported range is PostgreSQL 15 through 18 (decision D11).
// Minors are chosen above the reverted-ABI releases documented in R2.
var images = map[int]string{
	15: "postgres:15.14-alpine",
	16: "postgres:16.10-alpine",
	17: "postgres:17.6-alpine",
	18: "postgres:18.2-alpine",
}

// Versions returns the majors to exercise. PGLENS_PG_VERSIONS overrides it;
// the default is the PR matrix (the oldest and the newest supported), and
// CI sets the full range for the nightly run.
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
				continue
			}
			if _, ok := images[n]; ok {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []int{15, 18}
}

// ImageFor returns the docker image for a major version.
func ImageFor(major int) string {
	return images[major]
}
