package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const envFileProbe = "REVENIUM_TEST_ENV_FILE_PROBE"

func workInChildOfFolderWithEnvFile(t *testing.T, parentFileName, childFileName string) {
	t.Helper()
	parent := t.TempDir()
	child := filepath.Join(parent, "app")
	require.NoError(t, os.Mkdir(child, 0o755))
	if parentFileName != "" {
		writeEnvFile(t, filepath.Join(parent, parentFileName), "from-parent")
	}
	if childFileName != "" {
		writeEnvFile(t, filepath.Join(child, childFileName), "from-working-folder")
	}
	chdirForTest(t, child)
	unsetForTest(t, envFileProbe)
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func writeEnvFile(t *testing.T, path, value string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(envFileProbe+"="+value+"\n"), 0o600))
}

func unsetForTest(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

func TestLoadEnvFiles_NeverReadsTheParentFolder(t *testing.T) {
	for _, name := range envFileNames {
		t.Run(name, func(t *testing.T) {
			workInChildOfFolderWithEnvFile(t, name, "")

			LoadEnvFiles()

			_, set := os.LookupEnv(envFileProbe)
			assert.False(t, set, "a %s one folder above the working folder must not be loaded", name)
		})
	}
}

func TestLoadEnvFiles_ReadsTheWorkingFolder(t *testing.T) {
	for _, name := range envFileNames {
		t.Run(name, func(t *testing.T) {
			workInChildOfFolderWithEnvFile(t, "", name)

			LoadEnvFiles()

			assert.Equal(t, "from-working-folder", os.Getenv(envFileProbe))
		})
	}
}

func TestLoadEnvFiles_DisabledByFlag(t *testing.T) {
	for _, value := range []string{"true", "TRUE", "1"} {
		t.Run(value, func(t *testing.T) {
			workInChildOfFolderWithEnvFile(t, "", ".env")
			t.Setenv(EnvEnvFilesDisable, value)

			LoadEnvFiles()

			_, set := os.LookupEnv(envFileProbe)
			assert.False(t, set)
		})
	}
}

func TestLoadEnvFiles_KeepsVariablesAlreadySet(t *testing.T) {
	workInChildOfFolderWithEnvFile(t, "", ".env")
	t.Setenv(envFileProbe, "from-shell")

	LoadEnvFiles()

	assert.Equal(t, "from-shell", os.Getenv(envFileProbe))
}
