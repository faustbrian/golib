import { MerklePatriciaTrie } from "@ethereumjs/mpt";

const chunks = [];
for await (const chunk of process.stdin) {
  chunks.push(chunk);
}

const request = JSON.parse(Buffer.concat(chunks).toString("utf8"));
const trie = new MerklePatriciaTrie({ useKeyHashing: request.secure });
const results = [];

for (const operation of request.operations) {
  const key = Uint8Array.from(Buffer.from(operation.key, "hex"));
  if (operation.kind === "put") {
    const value = Uint8Array.from(Buffer.from(operation.value, "hex"));
    await trie.put(key, value);
  } else if (operation.kind === "delete") {
    await trie.del(key);
  } else {
    throw new Error(`unsupported operation kind: ${operation.kind}`);
  }

  const value = await trie.get(key);
  results.push({
    root: Buffer.from(trie.root()).toString("hex"),
    value: value === null ? null : Buffer.from(value).toString("hex"),
  });
}

process.stdout.write(JSON.stringify(results));
