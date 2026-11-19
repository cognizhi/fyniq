// The API's types, from the SDK (FR-INT-17): the one file generated from contracts/openapi/fyniq.v1.yaml is
// sdk/typescript/src/schema.d.ts (`pnpm --filter @fyniq/sdk gen`, after `make openapi`; `make test` fails while it is out
// of date). Types only: the web app reaches the API through ./client.
export type { components, operations, paths } from "@fyniq/sdk/schema";
