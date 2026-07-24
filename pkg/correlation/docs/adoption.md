# Adoption

Start at ingress. Construct one factory, choose an explicit immediate-peer
trust policy, install the HTTP wrapper, and read `Values` from request context.
At every outbound queue, RPC, or webhook boundary, call that adapter's send
method and propagate the returned values as the new parent for subsequent
work.

Roll out logging in redacted mode first. Verify that downstream systems do not
use existing request or correlation headers for authorization, tenancy, or
idempotency before enabling propagation. Add trusted proxies one by one and
keep untrusted traffic on replacement behavior.
