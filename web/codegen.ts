import type { CodegenConfig } from "@graphql-codegen/cli";

// graphql-codegen (client preset). Reads the server SDL directly — no running
// server needed — and emits TypedDocumentNodes into src/lib/gql/ so every
// `graphql(`…`)` call site is typed against the schema. A field/selection
// mismatch then becomes a svelte-check (compile-time) error instead of a
// silently-blank cell at runtime.
//
// The generated src/lib/gql/ folder is CHECKED IN (committed), NOT gitignored.
// Why commit generated output: the canonical web test command
// `cd web && npx vitest run` bypasses the npm `pretest`/`precheck` hooks, so it
// relies on the folder already existing on disk without a prior build/codegen.
// Committing it mirrors the project's already-committed gqlgen Go output
// (internal/graph/generated.go). It is still regenerated in-place before every
// check/test/build entry point (npm `pre*` hooks + the Taskfile
// `codegen`/`web:build` tasks), so a stale checked-in copy is overwritten by
// the gates before they run.
const config: CodegenConfig = {
  // Server SDL. graphql-codegen parses this file directly.
  schema: "../internal/graph/schema.graphqls",
  // All TS + Svelte sources; every `graphql(`…`)` literal is collected. The
  // generated output is excluded so codegen never re-scans its own docs map.
  documents: ["src/**/*.{ts,svelte}", "!src/lib/gql/**"],
  // Keep only the freshly generated output (no stale docs linger).
  ignoreNoDocuments: false,
  generates: {
    "src/lib/gql/": {
      preset: "client",
      presetConfig: {
        // Direct field access (data.nibs, data.nib, …) with no useFragment
        // wrapping — keeps this a drop-in typing migration.
        fragmentMasking: false,
      },
      config: {
        // The schema's only custom scalar. Map it to string so createdAt /
        // updatedAt line up with the existing NibSummary.createdAt: string.
        scalars: {
          Time: "string",
        },
      },
    },
  },
};

export default config;
