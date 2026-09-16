package nibcore

import (
	"bufio"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nib"
)

// The confirming read an area edit runs immediately before it writes the
// vocabulary: which nibs ON DISK are assigned at or below the path that write
// stops declaring.
//
// It answers that one question without installing a store. A full loadFromDisk
// answers it too — it is what this replaced — but it also retains every body,
// re-resolves every link, rebuilds the mention index and syncs the search index,
// none of which the question needs. Measured in situ at the point in the verb
// where the confirmation runs, medians of 3 per position, stores of 500/2000/5000
// nibs: 0.23-0.25x a load on Linux, 0.42-0.77x on Windows (nibs-upl4).
//
// IT MUST NOT UNDER-REPORT. A member it fails to see is a nib the vocabulary
// write strands on a path nothing declares, with every later write to it refused
// and nothing saying to rerun — the defect nibs-ptny exists to prevent. That is
// why the header is decoded by the same YAML parser nib.Parse answers to, rather
// than matched line by line the way cmd/migrate's detection scan does: that
// scan's own doc calls it "best-effort about VALUES", which is the wrong
// contract here, where a misread value costs a stranded nib rather than a wrong
// count.
//
// IT MUST NOT OVER-REPORT EITHER, which is a weaker rule with a sharper failure.
// This decodes one key and ignores every other rule the parser applies — a byte
// ceiling, a key ceiling, an alias budget — so a file nib.Parse REJECTS can
// still look like a member here. Counting one would refuse every retire and
// rename of that area permanently, naming a nib the store cannot load and
// prescribing a rerun that can never clear it.
//
// So the cheap read finds CANDIDATES and the parser decides them: a file whose
// header puts it at or below the path being asked about is parsed the way the
// load parses it before it joins the set. The confirmation is exact in both
// directions, and it costs nothing on the path that matters — the cascade has
// already moved every member off that path, so the usual candidate count is
// zero.

// maxAreaHeaderBytes bounds the bytes this spends looking for the closing fence.
//
// It is the PARSER's own ceiling on the block between the fences plus slack,
// because this read also spends bytes on the opening fence and on line endings:
// budgeting exactly nib.MaxFrontMatterBytes would fail to close a block that is
// just under it, which nib.Parse accepts — and skipping a file the load counts is
// the one direction this must never take. With the slack, a header that has not
// closed by here has a block over that ceiling, which nib.Parse refuses with an
// ordinary parse error and a load therefore skips, so skipping it here is parity
// rather than an under-report. (cmd/migrate's header scan reuses the same
// constant and counts the fences against it, which is why its cap is the
// stricter of the two.)
const maxAreaHeaderBytes = nib.MaxFrontMatterBytes + 4096

// areaHeaderBufBytes sizes the read buffer, like nib.Parse's bufio.Reader rather
// than like the ceiling above: a nib file is ~1.3 KB, and a buffer sized to the
// ceiling would pull a 1 MiB read per file.
const areaHeaderBufBytes = 8 * 1024

// areaOfFile returns the `area:` one nib file declares, reading only its front
// matter. ok is false for a file that is not a nib and for one nothing can be
// parsed out of — the same files a load skips.
func areaOfFile(path string, buf []byte) (string, bool, error) {
	f, err := OpenRegularFile(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(io.LimitReader(f, maxAreaHeaderBytes))
	sc.Buffer(buf, maxAreaHeaderBytes)
	if !sc.Scan() {
		return "", false, sc.Err()
	}
	// The fence rule nib.Parse applies, spelled the same way: a line IS a fence
	// iff TrimSpace equals the token. A stricter compare here reads BODY lines as
	// header keys.
	if fence := strings.TrimSpace(sc.Text()); fence != "---" && fence != "---yaml" {
		return "", false, nil
	}

	var header strings.Builder
	closed := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		header.WriteString(line)
		header.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	if !closed {
		// Past the parser's own ceiling (see maxAreaHeaderBytes): not a nib, and
		// not a file any load counted either.
		return "", false, nil
	}

	var fm struct {
		Area string `yaml:"area"`
	}
	if err := yaml.Unmarshal([]byte(header.String()), &fm); err != nil {
		// Unparseable front matter: the load skips this file too.
		return "", false, nil
	}
	return fm.Area, true, nil
}

// areaOfNibFile answers for one candidate through the parser the store loads
// with, so a file this scan would otherwise count is counted only if a load
// would have counted it. ok is false for anything nib.Parse refuses — which is
// exactly what a load does with such a file.
func areaOfNibFile(path string) (string, bool) {
	f, err := OpenRegularFile(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	b, err := nib.Parse(f)
	if err != nil {
		return "", false
	}
	return b.Area, true
}

// scanAreaMembersOnDiskLocked returns the ids of every nib ON DISK assigned at
// or below path, in id order, reading each file's front matter and installing
// nothing. Must be called with c.mu held.
//
// It walks what a load walks — data/ and archive/ — and skips what a load skips,
// so the set it reports is the set the load's own membership question would have
// reported, minus the work of becoming a store. The id comes from the FILENAME,
// as it does at load, so a file whose front matter disagrees with its name is
// named the way every other diagnostic names it.
func (c *Core) scanAreaMembersOnDiskLocked(areas *area.Vocabulary, path string) ([]string, error) {
	seen := make(map[string]struct{})
	buf := make([]byte, areaHeaderBufBytes)
	prefix := c.configPrefix()

	err := WalkStoreContent(c.layout, func(p string, walkErr error) error {
		if walkErr != nil {
			// One unreadable entry is not a broken store, exactly as at load.
			if errors.Is(walkErr, ErrNotRegularFile) {
				return nil
			}
			return walkErr
		}
		id, _ := nib.ParseFilename(filepath.Base(p), prefix)
		if id == "" {
			return nil
		}
		a, ok, err := areaOfFile(p, buf)
		if err != nil || !ok || !areas.IsWithin(a, path) {
			return nil
		}
		// A candidate, decided by the parser rather than by the cheap read: see
		// the over-reporting rule at the top of this file. Reached only for a
		// file that already looks like a member, which after the cascade is
		// normally none of them.
		confirmed, ok := areaOfNibFile(p)
		if !ok || !areas.IsWithin(confirmed, path) {
			return nil
		}
		seen[id] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Deduped by id: two files can carry one id, and the refusal names nibs.
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
