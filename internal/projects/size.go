package projects

import (
	"bufio"
	"bytes"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// bytesPerLine turns tracked bytes into an estimated line count: reading
// every file to count lines would cost more than the answer is worth
const bytesPerLine = 40

// skipped are files that aren't source, whatever their size
var skipped = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true, ".svg": true,
	".pdf": true, ".zip": true, ".gz": true, ".tar": true, ".jar": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".mp3": true, ".mp4": true, ".mov": true, ".wasm": true, ".so": true,
	".dylib": true, ".a": true, ".o": true, ".bin": true, ".lock": true, ".sum": true,
}

var skippedNames = map[string]bool{"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "Cargo.lock": true, "go.sum": true}

// Measure sizes up dir's committed source from git, without reading the
// working tree: git ls-tree lists each blob with its size, so this stays
// fast on big repos. Outside git, or before the first commit, it's empty
func Measure(dir string) (Size, error) {
	out, err := exec.Command("git", "-C", dir, "ls-tree", "-r", "-l", "--full-tree", "HEAD").Output()
	if err != nil {
		return Size{Measured: time.Now()}, nil
	}

	return parseTree(out), nil
}

// parseTree reads ls-tree -l lines: "<mode> blob <sha> <size>\t<path>"
func parseTree(out []byte) Size {
	s := Size{Measured: time.Now()}
	var total int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		meta, path, ok := strings.Cut(sc.Text(), "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 || f[1] != "blob" {
			continue
		}
		name := filepath.Base(path)
		if skippedNames[name] || skipped[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		n, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			continue
		}
		s.Files++
		total += n
	}
	s.LOC = total / bytesPerLine

	return s
}
