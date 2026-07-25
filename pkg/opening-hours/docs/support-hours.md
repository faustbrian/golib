# Support-hours adoption

Support desks often cross midnight and operate in a business timezone distinct
from the caller. Store that timezone explicitly and let the start date own an
overnight shift. Use dated subtraction for maintenance and replacement for an
exceptional local-time window.

Elapsed SLA calculations should use `OpenDuration` on an explicit instant
interval. The result correctly accounts for 23-hour and 25-hour civil days.
This package does not schedule agents, calculate payroll, or guarantee response
SLAs; it represents generic availability only.
