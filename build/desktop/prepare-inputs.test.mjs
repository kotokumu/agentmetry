import assert from "node:assert/strict";
import test from "node:test";

import { createNpmScriptInvocation, prepareDesktopInputs } from "./prepare-inputs.mjs";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const nextTurn = () => new Promise((resolve) => setImmediate(resolve));

test("runs npm scripts through the npm CLI with the current Node executable", () => {
  assert.deepEqual(
    createNpmScriptInvocation("desktop:icons", {
      nodeExecutable: "C:\\hostedtoolcache\\node.exe",
      npmCli: "C:\\hostedtoolcache\\node_modules\\npm\\bin\\npm-cli.js",
    }),
    {
      command: "C:\\hostedtoolcache\\node.exe",
      args: [
        "C:\\hostedtoolcache\\node_modules\\npm\\bin\\npm-cli.js",
        "run",
        "desktop:icons",
      ],
    },
  );
});

test("requires an npm lifecycle executable for desktop input builds", () => {
  assert.throws(
    () => createNpmScriptInvocation("desktop:icons", { npmCli: "" }),
    /npm_execpath is required/,
  );
});

test("builds icons in parallel with the web-to-sidecar dependency chain", async () => {
  const icons = deferred();
  const web = deferred();
  const sidecar = deferred();
  const events = [];
  let completed = false;

  const completion = prepareDesktopInputs({
    generateIcons: () => {
      events.push("icons");
      return icons.promise;
    },
    buildWeb: () => {
      events.push("web");
      return web.promise;
    },
    buildSidecar: () => {
      events.push("sidecar");
      return sidecar.promise;
    },
  }).then(() => {
    completed = true;
  });

  assert.deepEqual(events, ["icons", "web"]);

  icons.resolve();
  await nextTurn();
  assert.deepEqual(events, ["icons", "web"]);
  assert.equal(completed, false);

  web.resolve();
  await nextTurn();
  assert.deepEqual(events, ["icons", "web", "sidecar"]);
  assert.equal(completed, false);

  sidecar.resolve();
  await completion;
  assert.equal(completed, true);
});

test("does not build the sidecar when the web build fails", async () => {
  const icons = deferred();
  const webFailure = new Error("web build failed");
  let sidecarStarted = false;

  const completion = prepareDesktopInputs({
    generateIcons: () => icons.promise,
    buildWeb: async () => {
      throw webFailure;
    },
    buildSidecar: async () => {
      sidecarStarted = true;
    },
  });

  icons.resolve();
  await assert.rejects(completion, webFailure);
  assert.equal(sidecarStarted, false);
});
