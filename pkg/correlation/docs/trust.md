# Trust and precedence

Extraction validates syntax but assigns no trust. `InboundPolicy` separately
allows correlation preservation and conversion of an inbound request ID into
causation. Request IDs are never preserved because every hop and every retry
must receive a new one.

HTTP trust is an application callback over the immediate request. It should
accept only authenticated proxies or internal peers. Queue, scheduler, and
JSON-RPC adapters require an explicit boolean after the caller authenticates
the metadata boundary.

Conflicting duplicate values fail before precedence is considered. Malformed
trusted values fail; malformed HTTP values can be rejected or replaced as an
explicit policy. Injection never overwrites a populated carrier field.
