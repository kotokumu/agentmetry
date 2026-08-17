import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const text = async (path) => readFile(new URL(`../${path}`, import.meta.url), "utf8");
const binary = async (path) => readFile(new URL(`../${path}`, import.meta.url));

const pngMetadata = (contents) => ({
  signature: contents.subarray(0, 8).toString("hex"),
  width: contents.readUInt32BE(16),
  height: contents.readUInt32BE(20),
  colorType: contents[25],
});

test("ships transparent canonical brand artwork", async () => {
  const mark = pngMetadata(await binary("assets/brand/agentmetry-mark.png"));
  const logo = pngMetadata(await binary("assets/brand/agentmetry-logo.png"));

  for (const asset of [mark, logo]) {
    assert.equal(asset.signature, "89504e470d0a1a0a");
    assert.ok([4, 6].includes(asset.colorType), "brand artwork must carry an alpha channel");
  }
  assert.equal(mark.width, mark.height);
  assert.ok(mark.width >= 512);
  assert.ok(logo.width > logo.height);
});

test("README uses a theme-independent horizontal product logo", async () => {
  const readme = await text("README.md");
  const readmeLogo = pngMetadata(await binary("assets/brand/agentmetry-logo-readme.png"));

  assert.equal(readmeLogo.signature, "89504e470d0a1a0a");
  assert.ok(readmeLogo.width > readmeLogo.height);
  assert.match(readme, /<img src="\.\/assets\/brand\/agentmetry-logo-readme\.png" alt="Agentmetry"/);
});

test("web shell uses the canonical mark for the favicon and dashboard brand", async () => {
  const [canonicalMark, publicMark, index, app] = await Promise.all([
    binary("assets/brand/agentmetry-mark.png"),
    binary("web/public/agentmetry-mark.png"),
    text("web/index.html"),
    text("web/src/app/agentmetry-app.ts"),
  ]);

  const canonicalMetadata = pngMetadata(canonicalMark);
  const publicMetadata = pngMetadata(publicMark);
  assert.equal(publicMetadata.width, 256);
  assert.equal(publicMetadata.height, 256);
  assert.equal(publicMetadata.colorType, canonicalMetadata.colorType);
  assert.match(index, /<link rel="icon" type="image\/png" href="\/agentmetry-mark\.png"/);
  assert.match(app, /<img class="brand-mark" src="\/agentmetry-mark\.png" alt=""/);
});

test("desktop icon set is generated from the Agentmetry mark", async () => {
  const [canonicalMark, desktopIcon] = await Promise.all([
    binary("assets/brand/agentmetry-mark.png"),
    binary("src-tauri/icons/icon.png"),
  ]);
  const metadata = pngMetadata(desktopIcon);

  assert.equal(metadata.width, 512);
  assert.equal(metadata.height, 512);
  assert.notDeepEqual(desktopIcon, canonicalMark, "desktop icon should be a generated platform asset");
});
