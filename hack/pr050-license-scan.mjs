#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {existsSync, lstatSync, readdirSync, readFileSync, realpathSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const rootPackage = readPackageJSON(path.join(repoRoot, 'package.json'));
const nodeModules = path.join(repoRoot, 'node_modules');

const directNodeDependencyNames = new Set([
  ...Object.keys(rootPackage.dependencies ?? {}),
  ...Object.keys(rootPackage.optionalDependencies ?? {}),
]);

const goLicenseFilePattern = /^(?:license|licence|copying|notice)(?:[._-].*)?$/i;

function fail(message) {
  throw new Error(message);
}

function readPackageJSON(file) {
  return JSON.parse(readFileSync(file, 'utf8'));
}

function packageLicense(pkg) {
  if (typeof pkg.license === 'string') {
    return pkg.license.trim();
  }
  if (pkg.license && typeof pkg.license.type === 'string') {
    return pkg.license.type.trim();
  }
  if (Array.isArray(pkg.licenses)) {
    return pkg.licenses
      .map((license) => (typeof license === 'string' ? license : license?.type))
      .filter(Boolean)
      .join(' OR ');
  }
  return '';
}

function addInstalledPackageJSONs(modulesDir, out) {
  if (!existsSync(modulesDir)) {
    return;
  }
  for (const entry of readdirSync(modulesDir, {withFileTypes: true})) {
    if (!entry.isDirectory() && !entry.isSymbolicLink()) {
      continue;
    }
    if (entry.name === '.bin' || entry.name === '.pnpm') {
      continue;
    }
    const entryPath = path.join(modulesDir, entry.name);
    if (entry.name.startsWith('@')) {
      for (const scoped of readdirSync(entryPath, {withFileTypes: true})) {
        if (!scoped.isDirectory() && !scoped.isSymbolicLink()) {
          continue;
        }
        addPackageJSON(path.join(entryPath, scoped.name), out);
      }
      continue;
    }
    addPackageJSON(entryPath, out);
  }
}

function addPackageJSON(packageDir, out) {
  const packagePath = path.join(packageDir, 'package.json');
  if (!existsSync(packagePath)) {
    return;
  }
  if (lstatSync(packagePath).isSymbolicLink()) {
    out.add(realpathSync.native(packagePath));
    return;
  }
  out.add(packagePath);
}

function hasPermissiveOrAlternative(text) {
  const normalized = text.toUpperCase();
  return (
    /\bOR\b/.test(normalized) &&
    /\b(?:APACHE[- ]?2\.0|MIT|BSD[- ]?2[- ]CLAUSE|BSD[- ]?3[- ]CLAUSE|MPL[- ]?2\.0|ISC)\b/.test(normalized)
  );
}

function deniedLicenseNameFromExpression(text) {
  if (hasPermissiveOrAlternative(text)) {
    return '';
  }
  if (/\bAGPL(?:[- ]?\d+(?:\.\d+)*)?\b|GNU Affero General Public License/i.test(text)) {
    return 'AGPL';
  }
  if (/\bGPL(?:[- ]?\d+(?:\.\d+)*)?\b|GNU General Public License/i.test(text)) {
    return 'GPL';
  }
  return '';
}

function hasDeniedGoLicenseFile(fileName, text) {
  const spdx = text.match(/SPDX-License-Identifier:\s*([^\r\n]+)/i);
  if (spdx) {
    return deniedLicenseNameFromExpression(spdx[1]);
  }

  const normalizedFileName = fileName.toLowerCase();
  if (/(^|[._-])agpl($|[._-])/i.test(normalizedFileName)) {
    return 'AGPL';
  }
  if (/(^|[._-])gpl($|[._-])/i.test(normalizedFileName) && !/(^|[._-])lgpl($|[._-])/i.test(normalizedFileName)) {
    return 'GPL';
  }

  const heading = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(0, 6)
    .join('\n');
  if (hasPermissiveOrAlternative(heading)) {
    return '';
  }
  if (/GNU Affero General Public License/i.test(heading)) {
    return 'AGPL';
  }
  if (/GNU General Public License/i.test(heading) && !/GNU Lesser General Public License/i.test(heading)) {
    return 'GPL';
  }
  return '';
}

function scanNodePackages() {
  if (!existsSync(nodeModules)) {
    fail('node_modules is required for Node license scanning; run pnpm install first');
  }

  const packageJSONs = new Set();
  addInstalledPackageJSONs(nodeModules, packageJSONs);

  const pnpmStore = path.join(nodeModules, '.pnpm');
  if (existsSync(pnpmStore)) {
    for (const entry of readdirSync(pnpmStore, {withFileTypes: true})) {
      if (!entry.isDirectory()) {
        continue;
      }
      addInstalledPackageJSONs(path.join(pnpmStore, entry.name, 'node_modules'), packageJSONs);
    }
  }

  const packagesByNameVersion = new Map();
  for (const packagePath of packageJSONs) {
    const pkg = readPackageJSON(packagePath);
    const key = `${pkg.name ?? packagePath}@${pkg.version ?? '0.0.0'}`;
    packagesByNameVersion.set(key, {pkg, packagePath});
  }

  const denied = [];
  const missingDirect = [];
  const missingTransitive = [];

  for (const {pkg, packagePath} of packagesByNameVersion.values()) {
    const license = packageLicense(pkg);
    if (!license) {
      if (directNodeDependencyNames.has(pkg.name)) {
        missingDirect.push(pkg.name);
      } else {
        missingTransitive.push(pkg.name ?? path.relative(repoRoot, packagePath));
      }
      continue;
    }
    const deniedName = deniedLicenseNameFromExpression(license);
    if (deniedName) {
      denied.push(`${pkg.name}@${pkg.version ?? 'unknown'}: ${license} (${deniedName})`);
    }
  }

  if (missingDirect.length > 0) {
    fail(`direct Node dependencies missing license metadata: ${missingDirect.sort().join(', ')}`);
  }
  if (denied.length > 0) {
    fail(`denied Node dependency licenses found:\n${denied.sort().join('\n')}`);
  }

  return {
    packageCount: packagesByNameVersion.size,
    missingTransitiveCount: missingTransitive.length,
  };
}

function runGoList() {
  const result = spawnSync('go', ['list', '-m', '-json', 'all'], {
    cwd: repoRoot,
    encoding: 'utf8',
    shell: process.platform === 'win32',
  });
  if (result.status !== 0) {
    fail(`go list -m -json all failed:\n${result.stdout}\n${result.stderr}`);
  }
  return parseConcatenatedJSON(result.stdout);
}

function parseConcatenatedJSON(text) {
  const modules = [];
  const decoder = new TextDecoder();
  const bytes = new TextEncoder().encode(text);
  let offset = 0;
  while (offset < bytes.length) {
    while (offset < bytes.length && /\s/.test(decoder.decode(bytes.slice(offset, offset + 1)))) {
      offset++;
    }
    if (offset >= bytes.length) {
      break;
    }
    let depth = 0;
    let inString = false;
    let escape = false;
    let end = offset;
    for (; end < bytes.length; end++) {
      const char = decoder.decode(bytes.slice(end, end + 1));
      if (escape) {
        escape = false;
        continue;
      }
      if (char === '\\') {
        escape = inString;
        continue;
      }
      if (char === '"') {
        inString = !inString;
        continue;
      }
      if (inString) {
        continue;
      }
      if (char === '{') {
        depth++;
      } else if (char === '}') {
        depth--;
        if (depth === 0) {
          end++;
          break;
        }
      }
    }
    modules.push(JSON.parse(decoder.decode(bytes.slice(offset, end))));
    offset = end;
  }
  return modules;
}

function findGoLicenseFiles(moduleDir) {
  if (!moduleDir || !existsSync(moduleDir)) {
    return [];
  }
  return readdirSync(moduleDir, {withFileTypes: true})
    .filter((entry) => entry.isFile() && goLicenseFilePattern.test(entry.name))
    .map((entry) => path.join(moduleDir, entry.name));
}

function scanGoModules() {
  const modules = runGoList().filter((mod) => !mod.Main);
  const denied = [];
  const missingDirect = [];
  const missingTransitive = [];

  for (const mod of modules) {
    const licenseFiles = findGoLicenseFiles(mod.Dir);
    if (licenseFiles.length === 0) {
      if (mod.Indirect) {
        missingTransitive.push(`${mod.Path}@${mod.Version ?? 'unknown'}`);
      } else {
        missingDirect.push(`${mod.Path}@${mod.Version ?? 'unknown'}`);
      }
      continue;
    }

    for (const licenseFile of licenseFiles) {
      const licenseText = readFileSync(licenseFile, 'utf8');
      const deniedName = hasDeniedGoLicenseFile(path.basename(licenseFile), licenseText);
      if (deniedName) {
        denied.push(`${mod.Path}@${mod.Version ?? 'unknown'}: ${path.basename(licenseFile)} (${deniedName})`);
      }
    }
  }

  if (missingDirect.length > 0) {
    fail(`direct Go modules missing license files: ${missingDirect.sort().join(', ')}`);
  }
  if (denied.length > 0) {
    fail(`denied Go module licenses found:\n${denied.sort().join('\n')}`);
  }

  return {
    moduleCount: modules.length,
    missingTransitiveCount: missingTransitive.length,
  };
}

const goResult = scanGoModules();
console.log(`PR-050 Go dependency license scan passed for ${goResult.moduleCount} modules`);
if (goResult.missingTransitiveCount > 0) {
  console.log(`Transitive Go modules without license files: ${goResult.missingTransitiveCount}`);
}

const nodeResult = scanNodePackages();
console.log(`PR-050 Node dependency license scan passed for ${nodeResult.packageCount} installed packages`);
if (nodeResult.missingTransitiveCount > 0) {
  console.log(`Transitive Node packages without license metadata: ${nodeResult.missingTransitiveCount}`);
}
