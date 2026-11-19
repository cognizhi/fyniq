import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pages } from "./guide";

// The knowledge base's reader filter and this guide read each page's audience from the same front matter (PRD-001 AI-54,
// p1-knowledge-base design D2). test/kb/guide-audience.json is what the server's Go parser makes of every page; this checks
// the web app's parser gives the same key, roles and status for each, so what the assistant may cite to a person is what the
// guide shows them.
describe("the guide's audience", () => {
  it("is the same in the web app as on the server (AI-54)", () => {
    const server = JSON.parse(readFileSync(resolve(process.cwd(), "../../test/kb/guide-audience.json"), "utf8")) as {
      file: string;
      key: string;
      roles: string[];
      status: string;
    }[];
    const web = pages.map((p) => ({ file: p.file, key: p.key, roles: p.roles, status: p.draft ? "draft" : "published" }));
    expect(web.sort((a, b) => a.file.localeCompare(b.file))).toEqual(server.sort((a, b) => a.file.localeCompare(b.file)));
  });
});
