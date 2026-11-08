package main

import (
	"bytes"
	"os"
	"testing"
)

const multiqcFixture = "testdata/multiqc/multiqc_data.json"

func TestFixtureCarriesNoLocalPath(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(multiqcFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, home := range []string{"/home/", "/Users/"} {
		if bytes.Contains(content, []byte(home)) {
			t.Errorf("%s holds the local path %q", multiqcFixture, home)
		}
	}
}
