# Adoption guide

1. Inventory formats, payload lengths, GS1 use, render dimensions, and decode
   inputs in the existing system.
2. Query `barcode.CapabilityFor` and accept the documented limitations for each
   selected format. Do not convert an unadvertised format into a product claim.
3. Compare logical modules and decoded metadata against existing production
   fixtures. Pixel snapshots alone are insufficient.
4. Render at integer scale with the old and new pipelines, then decode the same
   image corpus with identical transformations and resource limits.
5. Set explicit decode limits from the application threat model.
6. Roll out per format and retain the previous encoder/decoder until observed
   decode and error rates meet the acceptance criteria.

Migration should preserve canonical payload bytes, check-digit ownership, GS1
separator behavior, quiet zones, and requested correction settings. Treat any
silent normalization difference as a compatibility defect.
