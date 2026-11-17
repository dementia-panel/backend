package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestReleaseResolution(t *testing.T) {
	for _, tc := range []struct{ name, url, curlExit, want string }{{"current", "https://github.com/dementia-panel/node/releases/tag/v0.1.0", "0", "https://raw.githubusercontent.com/dementia-panel/node/v0.1.0"}, {"new release", "https://github.com/dementia-panel/node/releases/tag/v0.2.0", "0", "https://raw.githubusercontent.com/dementia-panel/node/v0.2.0"}, {"unresolved latest", "https://github.com/dementia-panel/node/releases/latest", "0", ""}, {"unexpected repository", "https://github.com/other/node/releases/tag/v1", "0", ""}, {"invalid tag", "https://github.com/dementia-panel/node/releases/tag/../../main", "0", ""}, {"empty tag", "https://github.com/dementia-panel/node/releases/tag/", "0", ""}, {"download failure", "", "22", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte("#!/bin/sh\nprintf '%s' \"$TEST_RELEASE_URL\"\nexit \"$TEST_CURL_EXIT\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", "-c", "set -eu\n"+latestBundleScript+"printf '%s' \"$bundle_url\"")
			cmd.Env = append(os.Environ(), "PATH="+dir, "TEST_RELEASE_URL="+tc.url, "TEST_CURL_EXIT="+tc.curlExit)
			output, err := cmd.CombinedOutput()
			if tc.want == "" {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 71 {
					t.Fatalf("expected download failure, got %v: %s", err, output)
				}
			} else if err != nil || strings.TrimSpace(string(output)) != tc.want {
				t.Fatalf("got %q, error %v; want %q", output, err, tc.want)
			}
		})
	}
}
