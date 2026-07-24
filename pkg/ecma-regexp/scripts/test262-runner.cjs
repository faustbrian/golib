#!/usr/bin/env node

const childProcess = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = process.argv[2];
const bridge = process.argv[3];

if (!root || !bridge) {
  throw new Error('usage: test262-runner.cjs TEST262_ROOT BRIDGE');
}

const semanticDirectories = new Set([
  'dotall',
  'lookBehind',
  'match-indices',
  'named-groups',
  'property-escapes',
  'regexp-modifiers',
  'unicodeSets',
]);

const files = [];
walk(path.join(root, 'test', 'built-ins', 'RegExp'), (file) => {
  const relative = path.relative(
    path.join(root, 'test', 'built-ins', 'RegExp'),
    file,
  );
  const first = relative.split(path.sep)[0];
  const generatedProperty = relative.startsWith(
    `property-escapes${path.sep}generated${path.sep}`,
  ) && !relative.startsWith(
    `property-escapes${path.sep}generated${path.sep}strings${path.sep}`,
  );
  const source = fs.readFileSync(file, 'utf8');
  if (!generatedProperty &&
      !/\nnegative:\s*\n/.test(source) &&
      (semanticDirectories.has(first) || /^S15\.10\.2/.test(first))) {
    files.push(file);
  }
});
walk(path.join(root, 'test', 'language', 'literals', 'regexp'), (file) => {
  const source = fs.readFileSync(file, 'utf8');
  if (!/\nnegative:\s*\n/.test(source)) {
    files.push(file);
  }
});
files.sort();
if (process.env.TEST262_FILTER) {
  const filter = process.env.TEST262_FILTER;
  for (let index = files.length - 1; index >= 0; index--) {
    if (!files[index].includes(filter)) files.splice(index, 1);
  }
}

let calls = 0;
let matcherFiles = 0;
let builtInFiles = 0;
let builtInMatcherFiles = 0;
for (const file of files) {
  const source = fs.readFileSync(file, 'utf8');
  const callsBefore = calls;
  const builtIn = file.includes(`${path.sep}built-ins${path.sep}`);
  if (builtIn) builtInFiles++;
  const context = vm.createContext({
    Buffer,
    console,
    print() {},
    __bridge: bridge,
    __childProcess: childProcess,
    __recordCall() { calls++; },
  });

  load(context, path.join(root, 'harness', 'assert.js'));
  load(context, path.join(root, 'harness', 'sta.js'));
  for (const include of includes(source)) {
    load(context, path.join(root, 'harness', include));
  }
  vm.runInContext(wrapper(), context, { filename: 'ecma-regexp-wrapper.js' });
  try {
    vm.runInContext(source, context, { filename: file, timeout: 120_000 });
  } catch (error) {
    process.stderr.write(`${path.relative(root, file)}: ${error.stack}\n`);
    process.exit(1);
  }
  if (calls > callsBefore) {
    matcherFiles++;
    if (builtIn) builtInMatcherFiles++;
  }
}

process.stdout.write(
  `${JSON.stringify({
    files: files.length,
    matcherFiles,
    builtInFiles,
    builtInMatcherFiles,
    calls,
  })}\n`,
);

function walk(directory, visit) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(target, visit);
    } else if (entry.isFile() && entry.name.endsWith('.js')) {
      visit(target);
    }
  }
}

function includes(source) {
  const match = source.match(/^includes:\s*\[([^\]]*)\]/m);
  if (!match) return [];
  return match[1].split(',').map((item) => item.trim()).filter(Boolean);
}

function load(context, file) {
  vm.runInContext(fs.readFileSync(file, 'utf8'), context, { filename: file });
}

function wrapper() {
  return String.raw`
(() => {
  const encode = (value) => {
    const data = Buffer.allocUnsafe(value.length * 2);
    for (let index = 0; index < value.length; index++) {
      data.writeUInt16LE(value.charCodeAt(index), index * 2);
    }
    return data.toString('base64');
  };
  const decode = (value) => {
    const data = Buffer.from(value, 'base64');
    const chunks = [];
    const size = 8192;
    for (let offset = 0; offset < data.length; offset += size * 2) {
      const units = [];
      const end = Math.min(data.length, offset + size * 2);
      for (let index = offset; index < end; index += 2) {
        units.push(data.readUInt16LE(index));
      }
      chunks.push(String.fromCharCode(...units));
    }
    return chunks.join('');
  };
  const define = (object, key, value) => Object.defineProperty(object, key, {
    value,
    writable: true,
    enumerable: true,
    configurable: true,
  });
  RegExp.prototype.exec = function(input) {
    input = String(input);
    const request = JSON.stringify({
      source: encode(this.source),
      flags: this.flags,
      input: encode(input),
      lastIndex: Number(this.lastIndex),
    });
    const run = __childProcess.spawnSync(__bridge, [], {
      input: request,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      timeout: 60_000,
    });
    if (run.error) throw run.error;
    if (run.status !== 0) throw new Error(run.stderr || 'bridge failed');
    const answer = JSON.parse(run.stdout);
    if (answer.error) throw new Error(answer.error);
    this.lastIndex = answer.lastIndex;
    __recordCall();
    if (!answer.matched) return null;

    const result = answer.captures.map((capture) =>
      capture.value === null ? undefined : decode(capture.value)
    );
    define(result, 'index', answer.captures[0].start);
    define(result, 'input', input);
    const names = answer.nameOrder || [];
    define(result, 'groups', names.length === 0 ? undefined : Object.create(null));
    for (const name of names) {
      const capture = answer.names[name];
      define(result.groups, name, capture.value === null
        ? undefined
        : decode(capture.value));
    }
    if (this.hasIndices) {
      define(result, 'indices', answer.captures.map((capture) =>
        capture.value === null ? undefined : [capture.start, capture.end]
      ));
      define(
        result.indices,
        'groups',
        names.length === 0 ? undefined : Object.create(null),
      );
      for (const name of names) {
        const capture = answer.names[name];
        define(result.indices.groups, name, capture.value === null
          ? undefined
          : [capture.start, capture.end]);
      }
    }
    return result;
  };
})();
`;
}
