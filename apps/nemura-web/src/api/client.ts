// The one place the app reaches the network: the client generated from Nemura's contract (contracts/openapi/nemura/nemura.v1.yaml). The API is
// on this origin under /api behind Nemura's edge, and `make dev`'s Vite proxy does the same; the session is its cookie, never a header the app holds.
import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export const api = createClient<paths>({ baseUrl: "/api", credentials: "same-origin" });

export type Session = components["schemas"]["SessionOutputBody"];
export type SignInInfo = components["schemas"]["SignInOutputBody"];
export type Problem = components["schemas"]["Problem"];
