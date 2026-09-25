import { readdirSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { spawnSync } from "node:child_process";

function testFiles(directory) {
  const files = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...testFiles(path));
    } else if (entry.isFile() && entry.name.endsWith(".test.js")) {
      files.push(path);
    }
  }
  return files;
}

const files = testFiles("static/js")
  .map((path) => relative(process.cwd(), path).split(sep).join("/"))
  .sort();

if (files.length === 0) {
  console.error("FAIL: no JavaScript unit tests found under static/js");
  process.exit(1);
}

console.log("Running JavaScript tests:");
console.log(files.join("\n"));
const result = spawnSync(process.execPath, ["--test", ...files], { stdio: "inherit" });
process.exit(result.status ?? 1);
