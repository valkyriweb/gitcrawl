import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const builder = fileURLToPath(new URL("./build-docs-site.mjs", import.meta.url));

function buildFixture(t, markdown) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "renamed-checkout-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "docs"));
  fs.writeFileSync(path.join(root, "docs", "CNAME"), "gitcrawl.sh\n");
  fs.writeFileSync(path.join(root, "docs", "index.md"), `---\ntitle: Fixture\npermalink: /\n---\n${markdown}`);
  return {
    root,
    result: spawnSync(process.execPath, [builder], { cwd: root, encoding: "utf8", timeout: 20_000 }),
  };
}

test("documentation identity does not depend on checkout directory", (t) => {
  const { root, result } = buildFixture(t, "# Fixture\n");
  assert.equal(result.status, 0, result.stderr);
  const index = fs.readFileSync(path.join(root, "dist", "docs-site", "llms.txt"), "utf8");
  assert.match(index, /^# gitcrawl\n/);
  assert.match(index, /Source: https:\/\/github.com\/openclaw\/gitcrawl/);
});

test("same-page links must refer to an existing anchor", (t) => {
  const { result } = buildFixture(t, "# Fixture\n\n[Missing](#missing-section)\n");
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /missing anchor/);
});

test("valid and URL-encoded same-page anchors are accepted", (t) => {
  const { root, result } = buildFixture(t, "# Fixture\n\n[Plain](#existing-section) [Encoded](#%65xisting-section) [A & B](#existing-section)\n\n## Existing section\n");
  assert.equal(result.status, 0, result.stderr);
  const page = fs.readFileSync(path.join(root, "dist", "docs-site", "index.html"), "utf8");
  assert.ok(page.includes(">A &amp; B</a>"));
});


test("link URLs are HTML-escaped exactly once", (t) => {
  const { root, result } = buildFixture(t, "# Fixture\n\n[Query](https://example.test/?one=1&two=2)\n");
  assert.equal(result.status, 0, result.stderr);
  const page = fs.readFileSync(path.join(root, "dist", "docs-site", "index.html"), "utf8");
  assert.ok(page.includes('href="https://example.test/?one=1&amp;two=2"'));
});

test("the docs build preserves links, navigation and highlighted code", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gitcrawl-docs-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "docs"));
  fs.writeFileSync(path.join(root, "docs", "CNAME"), "gitcrawl.example.test\n");
  fs.writeFileSync(path.join(root, "docs", "index.md"), [
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
  fs.writeFileSync(
    path.join(root, "docs", "quickstart.md"),
    "---\npermalink: /guides/quickstart/\n---\n# Quickstart\n\n## Choose a command\n\n## Install & run\n",
  );
  const result = spawnSync(process.execPath, [builder], { cwd: root, encoding: "utf8", timeout: 20_000 });
  assert.equal(result.status, 0, result.stderr);
  const html = fs.readFileSync(path.join(root, "dist", "docs-site", "index.html"), "utf8");
  const article = html.slice(html.indexOf('<article class="doc'), html.indexOf("</article>"));
  assert.match(article, /class="home-actions"/);
  assert.match(article, /<h2 id="install-run">/);
  const guide = fs.readFileSync(path.join(root, "dist", "docs-site", "guides", "quickstart", "index.html"), "utf8");
  assert.match(guide, /class="toc-l2" href="#install-run">Install &amp; run<\/a>/);
  assert.match(article, /\bhref="guides\/quickstart\/(?:index\.html)?#choose-a-command"/);
  assert.match(article, /<table[\s>]/);
  assert.match(article, /<code[^>]*class="language-json"[^>]*>[\s\S]*?<span class="hl-key">&quot;enabled&quot;<\/span>/);
  assert.doesNotMatch(article, /href="javascript:|<script>/);
});
