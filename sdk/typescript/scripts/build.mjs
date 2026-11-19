// Builds dist/: the compiled client and the generated types, which tsc does not copy (a .d.ts is not emitted).
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
rmSync(join(root, "dist"), { recursive: true, force: true });
execFileSync("pnpm", ["exec", "tsc", "-p", "tsconfig.build.json"], { cwd: root, stdio: "inherit" });
mkdirSync(join(root, "dist"), { recursive: true });
copyFileSync(join(root, "src/schema.d.ts"), join(root, "dist/schema.d.ts"));
