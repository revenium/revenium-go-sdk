package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// Chdir makes dir the working directory until the test ends. The working
// directory belongs to the whole process, so a test that calls it must not
// run in parallel.
func Chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing the working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

// WorkBelowEnvFile writes contents to a .env file in a new folder and makes a
// subfolder of it the working directory, the layout of an app started from
// a shared build folder.
func WorkBelowEnvFile(t *testing.T, contents string) {
	t.Helper()
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatalf("writing the parent .env file: %v", err)
	}
	app := filepath.Join(parent, "app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatalf("creating the working folder: %v", err)
	}
	Chdir(t, app)
}
