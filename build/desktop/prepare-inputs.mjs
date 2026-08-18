import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const currentFile = fileURLToPath(import.meta.url);
const repositoryRoot = resolve(dirname(currentFile), "../..");
const npmCommand = process.platform === "win32" ? "npm.cmd" : "npm";

function runBuildCommand(label, command, args) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args, {
      cwd: repositoryRoot,
      stdio: "inherit",
    });

    child.once("error", rejectPromise);
    child.once("close", (code, signal) => {
      if (code === 0) {
        resolvePromise();
        return;
      }

      const outcome = signal ? `signal ${signal}` : `exit code ${code}`;
      rejectPromise(new Error(`${label} failed with ${outcome}`));
    });
  });
}

export async function prepareDesktopInputs({
  generateIcons = () => runBuildCommand("desktop icon generation", npmCommand, ["run", "desktop:icons"]),
  buildWeb = () => runBuildCommand("web build", npmCommand, ["run", "web:build"]),
  buildSidecar = () =>
    runBuildCommand("desktop sidecar build", process.execPath, ["build/desktop/build-sidecar.mjs"]),
} = {}) {
  const [icons, application] = await Promise.allSettled([
    generateIcons(),
    (async () => {
      await buildWeb();
      await buildSidecar();
    })(),
  ]);

  if (icons.status === "rejected") {
    throw icons.reason;
  }
  if (application.status === "rejected") {
    throw application.reason;
  }
}

if (process.argv[1] && resolve(process.argv[1]) === currentFile) {
  prepareDesktopInputs().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
