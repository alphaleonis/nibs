package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStore materializes a new-layout store under projectDir: a `.nibs`
// directory holding config.yml and data/<file> for each entry in files. It is
// the shape every nibs surface expects after the store-layout inversion — the
// `.nibs` DIRECTORY is the project marker, config lives inside it, and nib
// files live under data/.
func writeStore(t *testing.T, projectDir, configBody string, files map[string]string) string {
	t.Helper()
	storeDir := filepath.Join(projectDir, ".nibs")
	dataDir := filepath.Join(storeDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if configBody != "" {
		if err := os.WriteFile(filepath.Join(storeDir, "config.yml"), []byte(configBody), 0o644); err != nil {
			t.Fatalf("write store config: %v", err)
		}
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write nib file %s: %v", name, err)
		}
	}
	return storeDir
}

// TestStoreLayout_ListFromSubdirectory_TracerBullet is the whole inversion in
// one command: the store is located by walking up to the `.nibs` DIRECTORY
// (there is no `.nibs.yml` anywhere), its config is read from inside that
// directory, and its nibs are read from data/ — so `nibs list` run from a
// nested subdirectory lists the nib under the store's own prefix.
func TestStoreLayout_ListFromSubdirectory_TracerBullet(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetListFlags()

	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "demo")
	writeStore(t, projectDir, "nibs:\n  prefix: demo-\n  id_length: 4\n", map[string]string{
		"demo-a1b2--tracer.md": "---\nversion: 1\ntitle: Tracer\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	deep := filepath.Join(projectDir, "src", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir subdirectory: %v", err)
	}
	t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
	t.Setenv("NIBS_PATH", "")
	t.Chdir(deep)

	rootCmd.SetArgs([]string{"list", "--json"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("list from subdirectory: %v", err)
		}
	})

	env := parseListEnvelope(t, out)
	ids := envelopeIDs(env)
	if !ids["demo-a1b2"] {
		t.Errorf("list ids = %v, want demo-a1b2 (store discovered by .nibs directory, config read from inside it)", ids)
	}
}

// TestStoreLayout_NibsPathAppliesThatStoresConfig closes the wrong-prefix trap:
// pointing nibs at another project's store must apply THAT store's vocabulary,
// not the cwd project's. Before the config moved inside the store, --nibs-path
// relocated only the data directory while config discovery still walked up from
// the cwd, so a short id resolved under the neighboring project's prefix and a
// new nib was created with it.
func TestStoreLayout_NibsPathAppliesThatStoresConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetListFlags()

	tmpDir := t.TempDir()

	// Project A is the cwd, with a deliberately different prefix and id length.
	projectA := filepath.Join(tmpDir, "project-a")
	writeStore(t, projectA, "nibs:\n  prefix: aaa-\n  id_length: 8\n", map[string]string{
		"aaa-00000001--alpha.md": "---\nversion: 1\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nA.\n",
	})

	// Project B is the store we point at.
	projectB := filepath.Join(tmpDir, "project-b")
	storeB := writeStore(t, projectB, "nibs:\n  prefix: bbb-\n  id_length: 4\n", map[string]string{
		"bbb-b1b1--beta.md": "---\nversion: 1\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nB.\n",
	})

	t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
	t.Setenv("NIBS_PATH", "")
	t.Chdir(projectA)

	// A short id resolves only under the prefix of the store being read. `b1b1`
	// is B's nib; under A's `aaa-` prefix it names nothing.
	rootCmd.SetArgs([]string{"--nibs-path", storeB, "get", "b1b1", "-f", "id"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("get from store B: %v", err)
		}
	})
	if got := strings.TrimSpace(out); !strings.Contains(got, "bbb-b1b1") {
		t.Errorf("get b1b1 = %q, want it to resolve to bbb-b1b1 — --nibs-path must apply store B's prefix, not the cwd project's", got)
	}

	// The id length is the other half of the trap, and only a WRITE shows it:
	// A says 8, B says 4, so a nib created in B must get a 4-character id.
	resetRootPersistentFlags()
	bodyFile := filepath.Join(tmpDir, "body.md")
	if err := os.WriteFile(bodyFile, []byte("Body.\n"), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}
	rootCmd.SetArgs([]string{"--nibs-path", storeB, "new", "In B", "-d", "@" + bodyFile, "--json"})
	out = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new in store B: %v", err)
		}
	})
	var created struct {
		Nib struct {
			ID string `json:"id"`
		} `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("unmarshal new envelope: %v\nraw: %s", err, out)
	}
	if !strings.HasPrefix(created.Nib.ID, "bbb-") {
		t.Errorf("created id = %q, want the bbb- prefix of the store written to", created.Nib.ID)
	}
	if suffix := strings.TrimPrefix(created.Nib.ID, "bbb-"); len(suffix) != 4 {
		t.Errorf("created id = %q, want a 4-character suffix (store B's id_length), got %d", created.Nib.ID, len(suffix))
	}
}
