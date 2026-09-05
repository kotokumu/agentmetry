#!/usr/bin/env node

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const SUPPORTED_OPENSPEC_VERSION = '1.12.0';
const CHANGE_NAME = /^[a-z0-9][a-z0-9-]*$/;
const PATH_SEGMENT = '[a-z0-9]+(?:-[a-z0-9]+)*';
const CAPABILITY_PATH = new RegExp(`^${PATH_SEGMENT}(?:/${PATH_SEGMENT})*$`);
const ALLOWED_DELTA_SECTIONS = [
  /^Purpose$/i,
  /^(ADDED|MODIFIED|REMOVED|RENAMED) Requirements$/i,
];
const MODEL_SECTION = 'Main Spec Conceptual Model Replacements';

function unnumberedTitle(title) {
  return title.replace(/^\d+\.\s+/, '');
}

function abort(message) {
  throw new Error(message);
}

function findProjectRoot(start) {
  let current = path.resolve(start);
  while (true) {
    if (fs.existsSync(path.join(current, 'openspec', 'config.yaml'))) return current;
    const parent = path.dirname(current);
    if (parent === current) abort('Could not find openspec/config.yaml from the current directory.');
    current = parent;
  }
}

function runOpenSpec(root, args, { print = false } = {}) {
  const result = spawnSync('openspec', args, {
    cwd: root,
    encoding: 'utf8',
    stdio: print ? 'inherit' : 'pipe',
  });
  if (result.error) abort(`Failed to execute openspec: ${result.error.message}`);
  if (result.status !== 0) {
    const detail = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    abort(`openspec ${args.join(' ')} failed${detail ? `:\n${detail}` : '.'}`);
  }
  return (result.stdout ?? '').trim();
}

function structuralLines(content) {
  const lines = content.replace(/\r\n?/g, '\n').split('\n');
  const structural = [];
  let fence = null;
  let inComment = false;

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const trimmed = line.trim();

    if (fence) {
      if (new RegExp(`^ {0,3}${fence.char}{${fence.length},}\\s*$`).test(line)) fence = null;
      structural.push(false);
      continue;
    }

    if (inComment) {
      if (line.includes('-->')) inComment = false;
      structural.push(false);
      continue;
    }

    const commentStart = line.indexOf('<!--');
    if (commentStart !== -1) {
      if (!line.slice(commentStart + 4).includes('-->')) inComment = true;
      structural.push(false);
      continue;
    }

    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})(?:\s*\w+)?\s*$/);
    if (fenceMatch) {
      fence = { char: fenceMatch[1][0], length: fenceMatch[1].length };
      structural.push(false);
      continue;
    }

    structural.push(trimmed.length > 0);
  }

  return { lines, structural };
}

function levelTwoSections(content) {
  const { lines, structural } = structuralLines(content);
  const sections = [];
  for (let index = 0; index < lines.length; index += 1) {
    if (!structural[index]) continue;
    const match = lines[index].match(/^ {0,3}##\s+(.+?)\s*$/);
    if (match) sections.push({ title: match[1], index });
  }
  return { lines, sections };
}

function discoverSpecFiles(root) {
  const results = [];
  if (!fs.existsSync(root)) return results;

  const walk = (directory, segments) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      if (entry.name.startsWith('.') || entry.isSymbolicLink()) continue;
      const entryPath = path.join(directory, entry.name);
      if (entry.isDirectory()) walk(entryPath, [...segments, entry.name]);
      else if (entry.isFile() && entry.name === 'spec.md' && segments.length > 0) {
        results.push({ capability: segments.join('/'), file: entryPath });
      }
    }
  };

  walk(root, []);
  return results.sort((left, right) => left.capability.localeCompare(right.capability));
}

function checkDeltaSections(delta) {
  const content = fs.readFileSync(delta.file, 'utf8');
  const { sections } = levelTwoSections(content);
  for (const section of sections) {
    if (!ALLOWED_DELTA_SECTIONS.some((allowed) => allowed.test(section.title))) {
      abort(`${path.relative(process.cwd(), delta.file)} contains unsupported section ` +
        `"## ${section.title}". OpenSpec 1.12.0 would ignore it during archive.`);
    }
  }
  return content;
}

function stripHtmlComments(text) {
  return text.replace(/<!--[\s\S]*?-->/g, '');
}

function parseModelReplacements(modelFile) {
  const content = fs.readFileSync(modelFile, 'utf8');
  const { lines, sections } = levelTwoSections(content);
  const matches = sections.filter((section) => unnumberedTitle(section.title) === MODEL_SECTION);
  if (matches.length !== 1) abort(`model.md must contain exactly one "## ${MODEL_SECTION}" section.`);

  const start = matches[0].index + 1;
  const next = sections.find((section) => section.index >= start);
  const end = next?.index ?? lines.length;
  const sectionText = lines.slice(start, end).join('\n');
  const replacements = new Map();
  let cursor = 0;
  let noneDeclared = false;
  const sectionLines = stripHtmlComments(sectionText).split('\n');

  while (cursor < sectionLines.length) {
    const line = sectionLines[cursor];
    if (!line.trim() || /^-{3,}$/.test(line.trim())) {
      cursor += 1;
      continue;
    }
    if (line.trim() === 'None.') {
      noneDeclared = true;
      cursor += 1;
      continue;
    }
    const heading = line.match(/^###\s+`([^`]+)`\s*$/);
    if (!heading) {
      abort(`Unexpected content in "## ${MODEL_SECTION}": ${line.trim()}`);
    }

    const capability = heading[1];
    if (!CAPABILITY_PATH.test(capability)) abort(`Invalid capability path in model.md: ${capability}`);
    if (replacements.has(capability)) abort(`Duplicate Conceptual Model replacement for ${capability}.`);

    cursor += 1;
    while (cursor < sectionLines.length && !sectionLines[cursor].trim()) cursor += 1;
    if (sectionLines[cursor]?.trim() === 'REMOVE') {
      replacements.set(capability, { kind: 'remove' });
      cursor += 1;
      continue;
    }
    const opening = sectionLines[cursor]?.match(/^(`{3,}|~{3,})markdown\s*$/i);
    if (!opening) abort(`Conceptual Model replacement for ${capability} must use a markdown fenced block.`);
    const marker = opening[1];
    cursor += 1;
    const body = [];
    while (cursor < sectionLines.length && !new RegExp(`^${marker[0]}{${marker.length},}\\s*$`).test(sectionLines[cursor])) {
      body.push(sectionLines[cursor]);
      cursor += 1;
    }
    if (cursor >= sectionLines.length) abort(`Conceptual Model replacement for ${capability} has no closing fence.`);
    const replacement = body.join('\n').trim();
    if (!replacement) abort(`Conceptual Model replacement for ${capability} is empty.`);
    if (/^##\s+/m.test(replacement)) {
      abort(`Conceptual Model replacement for ${capability} must contain the section body only, without a level-two heading.`);
    }
    replacements.set(capability, { kind: 'replace', body: replacement });
    cursor += 1;
  }

  if (replacements.size === 0 && !noneDeclared) {
    abort(`"## ${MODEL_SECTION}" must contain "None." or one complete replacement per capability.`);
  }
  if (replacements.size > 0 && noneDeclared) {
    abort(`"## ${MODEL_SECTION}" cannot contain both "None." and replacements.`);
  }
  return { replacements, content };
}

function requireNoUnresolvedDecisions(modelContent) {
  const { lines, sections } = levelTwoSections(modelContent);
  const matches = sections.filter((section) => unnumberedTitle(section.title) === 'Unresolved Decisions');
  if (matches.length !== 1) abort('model.md must contain exactly one Unresolved Decisions section.');
  const start = matches[0].index + 1;
  const next = sections.find((section) => section.index >= start);
  const visible = stripHtmlComments(lines.slice(start, next?.index ?? lines.length).join('\n'))
    .split('\n')
    .filter((line) => !/^\s*-{3,}\s*$/.test(line))
    .join('\n')
    .trim();
  if (visible !== 'None.') abort('Unresolved Decisions must be exactly "None." before archive.');
}

function extractPurpose(deltaContent) {
  const { lines, sections } = levelTwoSections(deltaContent);
  const purpose = sections.find((section) => /^Purpose$/i.test(section.title));
  if (!purpose) return null;
  const next = sections.find((section) => section.index > purpose.index);
  return stripHtmlComments(lines.slice(purpose.index + 1, next?.index ?? lines.length).join('\n')).trim() || null;
}

function replaceConceptualModel(content, replacement, capability) {
  const { lines, sections } = levelTwoSections(content);
  const models = sections.filter((section) => /^Conceptual Model$/i.test(section.title));
  const requirements = sections.filter((section) => /^Requirements$/i.test(section.title));
  if (models.length > 1) abort(`${capability} has more than one Conceptual Model section.`);
  if (requirements.length !== 1) abort(`${capability} must have exactly one Requirements section.`);

  if (replacement.kind === 'remove') {
    if (models.length === 0) abort(`${capability} has no Conceptual Model section to remove.`);
    const model = models[0];
    const next = sections.find((section) => section.index > model.index);
    lines.splice(model.index, (next?.index ?? lines.length) - model.index);
    return `${lines.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd()}\n`;
  }

  const modelBlock = ['## Conceptual Model', '', replacement.body, ''];
  if (models.length === 0) {
    lines.splice(requirements[0].index, 0, ...modelBlock);
  } else {
    const model = models[0];
    const next = sections.find((section) => section.index > model.index);
    lines.splice(model.index, (next?.index ?? lines.length) - model.index, ...modelBlock);
  }
  return `${lines.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd()}\n`;
}

function newSpecSkeleton(capability, purpose, replacement) {
  if (!purpose) abort(`New capability ${capability} requires a non-empty Purpose in its delta spec.`);
  if (replacement.kind === 'remove') abort(`New capability ${capability} cannot remove a Conceptual Model.`);
  return `# ${capability} Specification\n\n## Purpose\n${purpose}\n\n` +
    `## Conceptual Model\n\n${replacement.body}\n\n## Requirements\n`;
}

function writeAtomically(file, content) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const temporary = `${file}.conceptual-model-${process.pid}`;
  fs.writeFileSync(temporary, content, 'utf8');
  fs.renameSync(temporary, file);
}

function pruneEmptyDirectories(start, stop) {
  let current = path.dirname(start);
  const boundary = path.resolve(stop);
  while (current.startsWith(`${boundary}${path.sep}`)) {
    if (fs.readdirSync(current).length > 0) break;
    fs.rmdirSync(current);
    current = path.dirname(current);
  }
}

function restoreSnapshots(snapshots, specsRoot) {
  for (const snapshot of snapshots.reverse()) {
    if (snapshot.existed) writeAtomically(snapshot.file, snapshot.content);
    else if (fs.existsSync(snapshot.file)) {
      fs.unlinkSync(snapshot.file);
      pruneEmptyDirectories(snapshot.file, specsRoot);
    }
  }
}

function main() {
  const [changeName, ...extra] = process.argv.slice(2);
  if (!changeName || extra.length > 0) abort('Usage: node tools/archive-change.mjs <change-name>');
  if (!CHANGE_NAME.test(changeName)) abort(`Invalid change name: ${changeName}`);

  const root = findProjectRoot(process.cwd());
  const version = runOpenSpec(root, ['--version']);
  if (version !== SUPPORTED_OPENSPEC_VERSION) {
    abort(`This publication command is verified for OpenSpec ${SUPPORTED_OPENSPEC_VERSION}; found ${version}.`);
  }

  const changeDir = path.join(root, 'openspec', 'changes', changeName);
  const modelFile = path.join(changeDir, 'model.md');
  const changeSpecsRoot = path.join(changeDir, 'specs');
  const mainSpecsRoot = path.join(root, 'openspec', 'specs');
  if (!fs.existsSync(changeDir)) abort(`Change not found: ${changeName}`);
  if (!fs.existsSync(modelFile)) abort(`Missing model.md for change ${changeName}.`);

  const deltas = discoverSpecFiles(changeSpecsRoot);
  const deltaByCapability = new Map();
  for (const delta of deltas) {
    if (!CAPABILITY_PATH.test(delta.capability)) abort(`Invalid capability path: ${delta.capability}`);
    delta.content = checkDeltaSections(delta);
    deltaByCapability.set(delta.capability, delta);
  }

  const parsedModel = parseModelReplacements(modelFile);
  const { replacements } = parsedModel;
  requireNoUnresolvedDecisions(parsedModel.content);
  for (const capability of replacements.keys()) {
    if (!deltaByCapability.has(capability)) {
      abort(`Conceptual Model replacement for ${capability} requires a delta spec at ` +
        `openspec/changes/${changeName}/specs/${capability}/spec.md.`);
    }
  }

  runOpenSpec(root, ['schema', 'validate', 'quality-spec']);
  runOpenSpec(root, ['validate', '--specs', '--strict', '--json']);
  runOpenSpec(root, ['validate', changeName, '--strict', '--json']);

  const status = JSON.parse(runOpenSpec(root, ['status', '--change', changeName, '--json']));
  if (!status.isPlanningComplete) abort(`Change ${changeName} has incomplete planning artifacts.`);
  const apply = JSON.parse(runOpenSpec(root, ['instructions', 'apply', '--change', changeName, '--json']));
  if (!apply.progress || apply.progress.total === 0) {
    abort(`Change ${changeName} has no implementation or verification tasks.`);
  }
  if (apply.progress?.remaining !== 0) {
    abort(`Change ${changeName} has ${apply.progress?.remaining ?? 'unknown'} incomplete task(s).`);
  }

  const snapshots = deltas.map((delta) => {
    const file = path.join(mainSpecsRoot, ...delta.capability.split('/'), 'spec.md');
    const existed = fs.existsSync(file);
    return { file, existed, content: existed ? fs.readFileSync(file, 'utf8') : null };
  });

  for (const delta of deltas) {
    const target = path.join(mainSpecsRoot, ...delta.capability.split('/'), 'spec.md');
    const purpose = extractPurpose(delta.content);
    if (fs.existsSync(target) && purpose) {
      abort(`Existing capability ${delta.capability} must omit Purpose from its delta spec; OpenSpec would ignore it.`);
    }
    if (!fs.existsSync(target) && !purpose) {
      abort(`New capability ${delta.capability} requires Purpose in its delta spec.`);
    }
  }

  try {
    for (const [capability, replacement] of replacements.entries()) {
      const delta = deltaByCapability.get(capability);
      const target = path.join(mainSpecsRoot, ...capability.split('/'), 'spec.md');
      const purpose = extractPurpose(delta.content);
      const staged = fs.existsSync(target)
        ? replaceConceptualModel(fs.readFileSync(target, 'utf8'), replacement, capability)
        : newSpecSkeleton(capability, purpose, replacement);
      writeAtomically(target, staged);
    }

    const archiveOutput = runOpenSpec(root, ['archive', changeName, '--yes', '--json']);
    const archiveResult = JSON.parse(archiveOutput);
    if (!archiveResult.archive?.path) abort('OpenSpec did not return an archive path.');

    try {
      runOpenSpec(root, ['validate', '--specs', '--strict', '--json']);
    } catch (validationError) {
      restoreSnapshots(snapshots, mainSpecsRoot);
      if (fs.existsSync(changeDir)) abort(`Post-archive validation failed and change path already exists: ${changeDir}`);
      fs.renameSync(archiveResult.archive.path, changeDir);
      throw validationError;
    }

    process.stdout.write(`${JSON.stringify(archiveResult, null, 2)}\n`);
  } catch (error) {
    if (fs.existsSync(changeDir)) restoreSnapshots(snapshots, mainSpecsRoot);
    throw error;
  }
}

try {
  main();
} catch (error) {
  process.stderr.write(`archive-change: ${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
}
