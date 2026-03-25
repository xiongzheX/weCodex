package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptRejectsNonDarwinPlatforms(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	tmp := t.TempDir()

	fakeBin := filepath.Join(tmp, "fake-bin")
	installDir := filepath.Join(tmp, "install-bin")
	mustMkdirAll(t, fakeBin)
	mustMkdirAll(t, installDir)

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/usr/bin/env bash
if [ "$1" = "-s" ]; then
  printf 'Linux\n'
  exit 0
fi
if [ "$1" = "-m" ]; then
  printf 'arm64\n'
  exit 0
fi
printf 'unexpected uname args: %s\n' "$*" >&2
exit 1
`)

	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+installDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install script to fail on non-Darwin platform, output: %s", string(out))
	}
	if !strings.Contains(string(out), "Darwin") {
		t.Fatalf("expected error to mention Darwin requirement, got: %s", string(out))
	}
}

func TestInstallScriptRejectsUnsupportedArchitecture(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	tmp := t.TempDir()

	fakeBin := filepath.Join(tmp, "fake-bin")
	installDir := filepath.Join(tmp, "install-bin")
	mustMkdirAll(t, fakeBin)
	mustMkdirAll(t, installDir)

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/usr/bin/env bash
if [ "$1" = "-s" ]; then
  printf 'Darwin\n'
  exit 0
fi
if [ "$1" = "-m" ]; then
  printf 'ppc64\n'
  exit 0
fi
printf 'unexpected uname args: %s\n' "$*" >&2
exit 1
`)

	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+installDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install script to fail on unsupported architecture, output: %s", string(out))
	}
	if !strings.Contains(strings.ToLower(string(out)), "unsupported") {
		t.Fatalf("expected unsupported architecture error, got: %s", string(out))
	}
}

func TestInstallScriptRequestsExpectedReleaseAsset(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	tmp := t.TempDir()

	fakeBin := filepath.Join(tmp, "fake-bin")
	installDir := filepath.Join(tmp, "install-bin")
	captureFile := filepath.Join(tmp, "curl-url.txt")
	mustMkdirAll(t, fakeBin)
	mustMkdirAll(t, installDir)

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/usr/bin/env bash
if [ "$1" = "-s" ]; then
  printf 'Darwin\n'
  exit 0
fi
if [ "$1" = "-m" ]; then
  printf 'arm64\n'
  exit 0
fi
printf 'unexpected uname args: %s\n' "$*" >&2
exit 1
`)

	writeExecutable(t, filepath.Join(fakeBin, "curl"), fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
url=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    http*)
      url="$1"
      shift
      ;;
    *)
      shift
      ;;
  esac
done
printf '%%s\n' "$url" > %q
if [ -n "$output" ]; then
  : > "$output"
fi
`, captureFile))

	writeExecutable(t, filepath.Join(fakeBin, "tar"), `#!/usr/bin/env bash
set -euo pipefail
dest="$PWD"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then
    dest="$2"
    shift 2
    continue
  fi
  shift
done
mkdir -p "$dest"
cat > "$dest/wecodex" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$dest/wecodex"
`)

	mustNoWriteDir(t, fakeBin)

	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+installDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected install script success, got error: %v, output: %s", err, string(out))
	}

	urlBytes, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read captured url: %v", err)
	}
	got := strings.TrimSpace(string(urlBytes))
	want := "https://github.com/xiongzheX/weCodex/releases/latest/download/wecodex-darwin-arm64.tar.gz"
	if got != want {
		t.Fatalf("unexpected release URL\nwant: %s\n got: %s", want, got)
	}
}

func TestInstallScriptSkipsNonWritablePathEntries(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	tmp := t.TempDir()

	nonWritableBin := filepath.Join(tmp, "non-writable-bin")
	writableBin := filepath.Join(tmp, "writable-bin")
	mustMkdirAll(t, nonWritableBin)
	mustMkdirAll(t, writableBin)

	writeExecutable(t, filepath.Join(nonWritableBin, "uname"), `#!/usr/bin/env bash
if [ "$1" = "-s" ]; then
  printf 'Darwin\n'
  exit 0
fi
if [ "$1" = "-m" ]; then
  printf 'arm64\n'
  exit 0
fi
printf 'unexpected uname args: %s\n' "$*" >&2
exit 1
`)

	writeExecutable(t, filepath.Join(nonWritableBin, "curl"), `#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    shift 2
    continue
  fi
  shift
done
if [ -n "$output" ]; then
  : > "$output"
fi
`)

	writeExecutable(t, filepath.Join(nonWritableBin, "tar"), `#!/usr/bin/env bash
set -euo pipefail
dest="$PWD"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then
    dest="$2"
    shift 2
    continue
  fi
  shift
done
mkdir -p "$dest"
cat > "$dest/wecodex" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$dest/wecodex"
`)

	mustNoWriteDir(t, nonWritableBin)

	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+nonWritableBin+":"+writableBin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected install script success, got error: %v, output: %s", err, string(out))
	}

	if _, err := os.Stat(filepath.Join(nonWritableBin, "wecodex")); err == nil {
		t.Fatalf("expected installer to skip non-writable PATH entry")
	}
	if _, err := os.Stat(filepath.Join(writableBin, "wecodex")); err != nil {
		t.Fatalf("expected wecodex installed into first writable PATH entry: %v", err)
	}
}

func TestInstallScriptInstallsWeCodexCompatibilityAlias(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	tmp := t.TempDir()

	fakeBin := filepath.Join(tmp, "fake-bin")
	installDir := filepath.Join(tmp, "install-bin")
	mustMkdirAll(t, fakeBin)
	mustMkdirAll(t, installDir)

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/usr/bin/env bash
if [ "$1" = "-s" ]; then
  printf 'Darwin\n'
  exit 0
fi
if [ "$1" = "-m" ]; then
  printf 'amd64\n'
  exit 0
fi
printf 'unexpected uname args: %s\n' "$*" >&2
exit 1
`)

	writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    shift 2
    continue
  fi
  shift
done
if [ -n "$output" ]; then
  : > "$output"
fi
`)

	writeExecutable(t, filepath.Join(fakeBin, "tar"), `#!/usr/bin/env bash
set -euo pipefail
dest="$PWD"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then
    dest="$2"
    shift 2
    continue
  fi
  shift
done
mkdir -p "$dest"
cat > "$dest/wecodex" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$dest/wecodex"
`)

	mustNoWriteDir(t, fakeBin)

	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+installDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected install script success, got error: %v, output: %s", err, string(out))
	}

	wecodexPath := filepath.Join(installDir, "wecodex")
	aliasPath := filepath.Join(installDir, "weCodex")

	if info, err := os.Stat(wecodexPath); err != nil {
		t.Fatalf("expected wecodex to be installed: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("expected wecodex to be executable")
	}

	if info, err := os.Stat(aliasPath); err != nil {
		t.Fatalf("expected compatibility alias weCodex to be installed: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("expected weCodex compatibility alias to be executable")
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root from %s", wd)
		}
		dir = parent
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustNoWriteDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o555); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o755)
	})
}
