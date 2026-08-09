# Operations and Kubernetes

## Normal shutdown

The caller owns process signals. On `SIGTERM` or a pod-eviction notice, stop
admission, cancel active input or iteration, wait for that store operation to
return, and call `Close`. An overlapping `Close` returns `ErrConcurrentUse`; it
does not interrupt or delete the active operation. Reserve enough termination
grace for cancellation, reader closure, and one bounded directory removal. A
cleanup error is operationally significant and should prevent a clean shutdown
claim.

`SIGKILL`, node loss, runtime failure, and termination-grace expiry cannot run
Go defers or `Close`. Encrypted work directories can remain after those events.
This is declared crash residue, not in-process cleanup.

## Caller-owned stale-directory janitor

Use a dedicated trusted cleanup root. Give each process or pod a separate
owner namespace beneath that root, preferably named from an immutable pod UID,
and configure the namespace itself as `ParentDirectory`. Record the owner UID,
creation time, and a renewable lease outside the module's hidden work
directories. Age alone is not proof that an owner is dead.

A janitor should perform this sequence:

1. Open the configured cleanup root as a directory without following a final
   symlink (`O_DIRECTORY|O_NOFOLLOW` on Linux), then verify its expected device,
   inode, owner, and owner-only mode.
2. Enumerate owner namespaces relative to that open descriptor. Reject absolute
   names, separators, dot entries, unexpected prefixes, and every entry that is
   not a non-symlink directory according to `fstatat(...,
   AT_SYMLINK_NOFOLLOW)` or an equivalent platform primitive.
3. Require the expected operating-system owner and a caller-issued ownership
   record. Confirm through the workload controller that the pod or process no
   longer exists, the lease is expired, and a conservative minimum age has
   elapsed. Treat missing or ambiguous ownership evidence as live.
4. Reopen the candidate relative to the root descriptor with non-following
   semantics. Recheck device and inode against the enumerated entry immediately
   before deletion to close rename and replacement races.
5. Remove entries descriptor-relatively without following symlinks. Never feed
   a joined, attacker-changeable pathname to an unrestricted recursive delete.
   Remove the now-empty owner namespace through the already-open root
   descriptor.
6. Record the ownership decision and result without logging keys, records,
   chunk contents, or full sensitive paths. Retry failures; do not broaden the
   root or relax non-following checks.

The janitor owns its rename and deletion failure campaigns. The external-sort
module itself does not rename chunks: a chunk becomes merge-visible only after
its unique temporary file is fully written, synchronized, and closed.

## Kubernetes storage and eviction

- Mount a writable dedicated volume for `ParentDirectory`; a read-only image
  root or read-only volume causes `Open` or spill to fail closed.
- Size `emptyDir.sizeLimit`, pod ephemeral-storage requests, and limits from
  `MaximumRecords * (RecordBytes + GCM nonce bytes + GCM tag bytes)`, plus
  filesystem metadata and the application's non-sort usage. Quota, inode, and
  disk exhaustion surface as `ErrStorage`; the caller should cancel work and
  call `Close`.
- Install the normal-shutdown sequence for both direct `SIGTERM` and eviction.
  Set `terminationGracePeriodSeconds` from measured worst-case cancellation and
  cleanup time. Grace expiry can leave the declared residue above.
- On a reused persistent or local volume, run the ownership-aware janitor before
  admitting a new pod. Never delete by age or `.external-sort-*` name alone.
- Monitor cleanup failures, remaining owner namespaces, temporary bytes, and
  eviction pressure. Do not infer plaintext exposure merely from encrypted
  residue, but treat residue metadata and retained ciphertext as sensitive.
