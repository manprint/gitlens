//go:build integration

package pgtest

import (
	"fmt"
	"testing"
)

// postgisImages are the PostGIS images, by PostgreSQL major. Their init
// script installs postgis, postgis_topology, fuzzystrmatch and
// postgis_tiger_geocoder in the test database.
var postgisImages = map[int]string{
	17: "postgis/postgis:17-3.6-alpine",
	18: "postgis/postgis:18-3.6-alpine",
	19: "postgis/postgis:19rc1-3.6-alpine",
}

var postgisByMajor = map[int]*PG{}

// ForEachPostGIS runs fn once per version in Versions() that has a PostGIS
// image, as a subtest named "postgis18", ..., against a container with
// PostGIS installed in the app database. It skips when none of the selected
// versions has one.
func ForEachPostGIS(t *testing.T, fn func(t *testing.T, pg *PG)) {
	t.Helper()
	ran := false
	for _, major := range Versions() {
		img, ok := postgisImages[major]
		if !ok {
			continue
		}
		ran = true
		major := major
		t.Run(fmt.Sprintf("postgis%d", major), func(t *testing.T) {
			t.Parallel()
			fn(t, getOrCreateImage(t, major, img, postgisByMajor))
		})
	}
	if !ran {
		t.Skipf("no PostGIS image for PostgreSQL %v", Versions())
	}
}
