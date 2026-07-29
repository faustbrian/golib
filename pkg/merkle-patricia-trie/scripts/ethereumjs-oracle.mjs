import { MerklePatriciaTrie } from "@ethereumjs/mpt";
import { RLP } from "@ethereumjs/rlp";

const chunks = [];
for await (const chunk of process.stdin) {
  chunks.push(chunk);
}

const request = JSON.parse(Buffer.concat(chunks).toString("utf8"));

if (request.mode === "rlp") {
  const values = request.values ?? [];
  const encodings = values.map((value) =>
    Buffer.from(RLP.encode(decodeRLPValue(value))).toString("hex"),
  );
  const accepted = (request.encodings ?? []).map((encoded) => {
    try {
      RLP.decode(Uint8Array.from(Buffer.from(encoded, "hex")));
      return true;
    } catch {
      return false;
    }
  });
  process.stdout.write(JSON.stringify({ encodings, accepted }));
} else {
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
}

function decodeRLPValue(value) {
  if (value.kind === "string") {
    return Uint8Array.from(Buffer.from(value.bytes ?? "", "hex"));
  }
  if (value.kind === "list") {
    return (value.elements ?? []).map(decodeRLPValue);
  }
  throw new Error(`unsupported RLP value kind: ${value.kind}`);
}
