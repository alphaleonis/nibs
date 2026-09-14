import type { CodegenConfig } from "@graphql-codegen/cli";

// graphql-codegen (client preset): types every `graphql(`…`)` call site against
// the server SDL, with no running server.
//
// src/lib/gql/ is committed, because a bare `npx vitest run` skips the npm
// `pre*` codegen hooks and needs it on disk. `task test` and CI build first,
// which regenerates it.
const config: CodegenConfig = {
  schema: "../internal/graph/schema.graphqls",
  documents: ["src/**/*.{ts,svelte}", "!src/lib/gql/**"],
  // Fail when no documents are found.
  ignoreNoDocuments: false,
  generates: {
    "src/lib/gql/": {
      preset: "client",
      presetConfig: {
        // Direct field access (data.nibs), no useFragment unwrapping.
        fragmentMasking: false,
      },
      config: {
        // Matches the hand-written NibSummary.createdAt: string.
        scalars: {
          Time: "string",
        },
      },
    },
  },
};

export default config;
