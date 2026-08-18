import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const currentFile = fileURLToPath(import.meta.url);
const repositoryRoot = resolve(dirname(currentFile), "../..");

export function createNpmScriptInvocation(
  script,
  {
    nodeExecutable = process.execPath,
    npmCli = process.env.npm_execpath,
  } = {},
) {
  if (!npmCli) {
    throw new Error("npm_execpath is required to run desktop input builds");
  }

  return {
    command: nodeExecutable,
    args: [npmCli, "run", script],
  };
}

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

function runNpmScript(label, script) {
  const { command, args } = createNpmScriptInvocation(script);
  return runBuildCommand(label, command, args);
}

export async function prepareDesktopInputs({
  generateIcons = () => runNpmScript("desktop icon generation", "desktop:icons"),
  buildWeb = () => runNpmScript("web build", "web:build"),
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
