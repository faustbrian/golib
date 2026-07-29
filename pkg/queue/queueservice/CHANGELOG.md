# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

### Changed

- Publish the service lifecycle adapter as an independently versioned optional
  module so core queue consumers do not inherit service or correlation runtime
  dependencies.

### Added

- Correlation-aware producer, delivery-handler, and worker lifecycle adapters
  for the owned queue and service modules.
