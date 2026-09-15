// Package yamlfile holds the plumbing every nibs YAML file shares: the size cap,
// the bounded regular-file read, single-document decoding, mapping lookup and the
// mode-preserving write.
//
// Keep it a leaf — stdlib, yaml.v3 and internal/fsutil only: internal/config,
// internal/area and internal/nibcore all import it, and nibcore imports the
// other two.
package yamlfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// MaxBytes bounds every config file read. A nibs config is a few dozen lines,
// and several of these reads sit on the ordinary path of an everyday command,
// where an unbounded os.ReadFile would turn one oversized file into several
// times its size in resident memory. Same posture as nib.MaxFrontMatterBytes for
// a nib's header.
const MaxBytes = 1 << 20 // 1 MiB

// ReadFile reads a config file, refusing one that is not a regular file and one
// larger than MaxBytes. Read every config file through it: it is the one point
// they all pass through.
//
// THE REGULARITY CHECK IS ABOUT LIVENESS. Opening a FIFO for reading blocks
// inside open(2) until a writer arrives, so a `.nibs.yml` or config.yml that is
// a named pipe hangs the command instead of failing it, and nothing downstream
// can bound that — the process never reaches downstream. Statting first answers
// before the open. It also makes the answer DETERMINATE: the discovery route
// reads the same pre-layout `.nibs.yml` twice (cmd/root.go), and a FIFO can
// serve different bytes to each read.
//
// The stat races the filesystem by construction. This guard bounds a hang and a
// divergence, not an attacker — do not treat "was regular a moment ago" as a
// security property.
//
// The ceiling is enforced by reading one byte PAST it and erroring. Never
// truncate instead: a shortened config parses as a different project, and a
// missing prefix re-prefixes every new nib. A missing file comes back as an
// ordinary os.IsNotExist error, so callers can keep treating absence as "use
// the defaults".
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is %s, not a regular file; a nibs config is an ordinary file, and reading a pipe or a device here would block the command instead of failing — remove or replace it",
			path, describeFileKind(info.Mode()))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%s is larger than the %d-byte configuration limit; a nibs config is a few dozen lines, so this is either not a config or is corrupt",
			path, MaxBytes)
	}
	return data, nil
}

// describeFileKind names what sits at a path a config was expected at. A stray
// FIFO and a directory called config.yml are different mistakes with different
// fixes, so the refusal quotes this rather than saying "not a regular file".
func describeFileKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe (FIFO)"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeCharDevice != 0:
		return "a character device"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	default:
		return "of type " + mode.Type().String()
	}
}

// ErrMultipleDocuments reports a file that holds more than one YAML document.
// Every in-place editor refuses it and words its own remedy, which has to name
// the edit that would have rewritten the file from the first document alone.
var ErrMultipleDocuments = errors.New("more than one YAML document")

// SoleDocument decodes data as the single YAML document a nibs config is. That
// is what makes an in-place edit of one key safe to write back: yaml.Marshal
// re-emits the file from one node tree, so a second document would be deleted by
// the write carrying the edit.
//
// An empty file comes back as a ZERO NODE rather than an error — callers differ
// on it, so each decides. Anything else the decoder objects to is returned as it
// came, for the caller to word.
func SoleDocument(data []byte) (yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	switch err := decoder.Decode(&doc); {
	case errors.Is(err, io.EOF):
		return yaml.Node{}, nil
	case err != nil:
		return yaml.Node{}, err
	}
	var next yaml.Node
	switch err := decoder.Decode(&next); {
	case err == nil:
		return yaml.Node{}, ErrMultipleDocuments
	case !errors.Is(err, io.EOF):
		return yaml.Node{}, err
	}
	return doc, nil
}

// MappingValue returns the value node for key in a YAML mapping, or nil.
func MappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// WritePreservingMode writes data over the file at path, keeping the existing
// file's permissions and reporting a replaced symlink. A file that has never
// existed gets 0644; a stat failure other than absence is returned rather than
// defaulted, since a default could only widen a narrower real mode.
func WritePreservingMode(path string, data []byte) (staleLinkTarget string, err error) {
	if link, lstatErr := os.Lstat(path); lstatErr == nil && link.Mode()&os.ModeSymlink != 0 {
		if target, readErr := os.Readlink(path); readErr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			staleLinkTarget = target
		} else {
			staleLinkTarget = path
		}
	}
	perm := os.FileMode(0644)
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		perm = info.Mode().Perm()
	case !errors.Is(statErr, fs.ErrNotExist):
		return "", fmt.Errorf("reading the current mode of %s: %w", path, statErr)
	}
	if err := fsutil.AtomicWriteFile(path, data, perm); err != nil {
		return "", err
	}
	return staleLinkTarget, nil
}
