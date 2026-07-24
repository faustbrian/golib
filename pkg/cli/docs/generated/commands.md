# `tool` command reference

## `tool`

Canonical cli reference application

### Options

- `-v`, `--verbose` (`bool`, persistent): enable diagnostic output

### `tool deploy`

Deploy an application

Deploy an application to an explicit target.

Documentation: https://example.com/tool/deploy

### Arguments

- `target` (`string`, required): deployment target
- `extra` (`string-slice`, repeated): additional immutable argv

### Options

- `--format` (`enum`, defaulted): select output format Allowed values: `human`, `json`.
- `--timeout` (`duration`, defaulted): bound operation duration
- `-v`, `--verbose` (`bool`, persistent): enable diagnostic output Inherited from `tool`.

### Aliases

- `ship`

### Examples

    tool deploy production --format json
