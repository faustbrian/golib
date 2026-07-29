import {
  MerklePatriciaTrie,
  createMerkleProof,
  verifyMPTWithMerkleProof,
} from "@ethereumjs/mpt";

const chunks = [];
for await (const chunk of process.stdin) {
  chunks.push(chunk);
}

const request = JSON.parse(Buffer.concat(chunks).toString("utf8"));
const trie = new MerklePatriciaTrie({ useKeyHashing: request.secure });

for (const operation of request.operations) {
  const key = Uint8Array.from(Buffer.from(operation.key, "hex"));
  if (operation.kind !== "put") {
    throw new Error(`unsupported operation kind: ${operation.kind}`);
  }
  const value = Uint8Array.from(Buffer.from(operation.value, "hex"));
  await trie.put(key, value);
}

const key = Uint8Array.from(Buffer.from(request.key, "hex"));
const proof = await createMerkleProof(trie, key);
const generatedValue = await verifyMPTWithMerkleProof(
  trie,
  trie.root(),
  key,
  proof,
);

let providedValue = null;
if (request.proof !== null) {
  const providedProof = request.proof.map((node) =>
    Uint8Array.from(Buffer.from(node, "hex")),
  );
  providedValue = await verifyMPTWithMerkleProof(
    trie,
    trie.root(),
    key,
    providedProof,
  );
}

process.stdout.write(
  JSON.stringify({
    root: Buffer.from(trie.root()).toString("hex"),
    proof: proof.map((node) => Buffer.from(node).toString("hex")),
    generatedValue:
      generatedValue === null
        ? null
        : Buffer.from(generatedValue).toString("hex"),
    providedValue:
      providedValue === null
        ? null
        : Buffer.from(providedValue).toString("hex"),
  }),
);
