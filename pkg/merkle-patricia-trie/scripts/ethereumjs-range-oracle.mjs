import {
  createMerkleProof,
  MerklePatriciaTrie,
  verifyMerkleRangeProof,
} from "@ethereumjs/mpt";

const chunks = [];
for await (const chunk of process.stdin) {
  chunks.push(chunk);
}

const request = JSON.parse(Buffer.concat(chunks).toString("utf8"));
const fromHex = (value) => Uint8Array.from(Buffer.from(value, "hex"));
const trie = new MerklePatriciaTrie();
for (let index = 0; index < request.stateKeys.length; index++) {
  await trie.put(
    fromHex(request.stateKeys[index]),
    fromHex(request.stateValues[index]),
  );
}
if (Buffer.from(trie.root()).toString("hex") !== request.root) {
  throw new Error("ethereumjs state root mismatch");
}
const edgeProof = [];
const seen = new Set();
for (const key of [request.firstKey, request.lastKey]) {
  for (const encoded of await createMerkleProof(trie, fromHex(key))) {
    const hex = Buffer.from(encoded).toString("hex");
    if (!seen.has(hex)) {
      seen.add(hex);
      edgeProof.push(encoded);
    }
  }
}
const witnessNodes = new Set(request.proof);
if (!edgeProof.every((encoded) => witnessNodes.has(Buffer.from(encoded).toString("hex")))) {
  throw new Error("generated witness omits an ethereumjs edge node");
}
const hasMore = await verifyMerkleRangeProof(
  fromHex(request.root),
  fromHex(request.firstKey),
  fromHex(request.lastKey),
  request.keys.map(fromHex),
  request.values.map(fromHex),
  edgeProof,
);

process.stdout.write(JSON.stringify({ hasMore, edgeNodesMatched: true }));
