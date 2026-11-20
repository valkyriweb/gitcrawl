import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const build = fileURLToPath(new URL("./build-docs-site.mjs", import.meta.url));

test("the docs build preserves links, navigation and highlighted code", (t) => {
  const root = mkdtempSync(join(tmpdir(), "gitcrawl-docs-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs"));
  writeFileSync(join(root, "docs/CNAME"), "gitcrawl.example.test\n");
  writeFileSync(join(root, "docs/index.md"), [
    "# Gitcrawl",
    "",
    "[Quickstart](quickstart.md) [View on GitHub](https://github.com/openclaw/gitcrawl)",
    "",
    "## Install & run",
    "",
    "[Choose a command](/guides/quickstart/#choose-a-command) and [bad](javascript:bad).",
    "",
    "<script>alert('raw HTML is not trusted')</script>",
    "",
    "## Output",
    "",
    "| Name | Value |",
    "| --- | ---: |",
    "| Test | 1 |",
    "",
    "~~~json",
    '{"enabled": true}',
    "~~~",
  ].join("\n"));
  writeFileSync(
    join(root, "docs/quickstart.md"),
    "---\npermalink: /guides/quickstart/\n---\n# Quickstart\n\n## Choose a command\n\n## Install & run\n",
  );
  execFileSync(process.execPath, [build], { cwd: root });
  const html = readFileSync(join(root, "dist/docs-site/index.html"), "utf8");
  const article = html.slice(html.indexOf('<article class="doc'), html.indexOf("</article>"));
  assert.match(article, /class="home-actions"/);
  assert.match(article, /<h2 id="install-run">/);
  const guide = readFileSync(join(root, "dist/docs-site/guides/quickstart/index.html"), "utf8");
  assert.match(guide, /class="toc-l2" href="#install-run">Install &amp; run<\/a>/);
  assert.match(article, /\bhref="guides\/quickstart\/(?:index\.html)?#choose-a-command"/);
  assert.match(article, /<table[\s>]/);
  assert.match(article, /<code[^>]*class="language-json"[^>]*>[\s\S]*?<span class="hl-key">&quot;enabled&quot;<\/span>/);
  assert.doesNotMatch(article, /href="javascript:|<script>/);
});
