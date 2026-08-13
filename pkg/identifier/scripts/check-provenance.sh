#!/usr/bin/env bash
set -euo pipefail

test "$(go env GOVERSION)" = "go1.26.6"
test -z "$(go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all)"
go mod verify
git diff --exit-code -- go.mod go.sum

test "$(shasum -a 256 typeid/testdata/official/valid.json | cut -d' ' -f1)" = \
  "af5a9cf2447d757b9354f33861d5d83f4e3244487eac191e37021013ce0c17e3"
test "$(shasum -a 256 typeid/testdata/official/invalid.json | cut -d' ' -f1)" = \
  "b1bf19bd2c922970bbe0381499807d3a0da652abb3da813b9ab75bac7c25217c"
rg -F \
  '// Source-map SHA-256: e089edb1b32df867bf27f3bca224228f49a850a75bfa4f693d293306e83db72c' \
  slug/transliteration_table.go >/dev/null
test "$(shasum -a 256 slug/testdata/laravel_english.json | cut -d' ' -f1)" = \
  "56cc72e173eed7efb6b6ae6f738bcdde075c4f0e66e54190e89fdff40fbc88e9"
awk -F '\t' '
  NR == 1 {
    if ($0 != "family\tkind\tlanguage\tsource\trevision\tvector") exit 1
    next
  }
  NF != 6 { exit 1 }
  $1 == "uuid" || $1 == "ulid" || $1 == "typeid" || $1 == "ksuid" ||
    $1 == "nanoid" || $1 == "slug" { seen[$1]++ }
  $1 == "typeid" && $2 == "official-spec-valid" &&
    $5 == "be8ff0daf5dc1f6d40c62a03cfc89945263a69af" &&
    $6 == "sha256:af5a9cf2447d757b9354f33861d5d83f4e3244487eac191e37021013ce0c17e3" {
      typeid_valid++
    }
  $1 == "typeid" && $2 == "official-spec-invalid" &&
    $5 == "be8ff0daf5dc1f6d40c62a03cfc89945263a69af" &&
    $6 == "sha256:b1bf19bd2c922970bbe0381499807d3a0da652abb3da813b9ab75bac7c25217c" {
      typeid_invalid++
    }
  $1 == "slug" && $2 == "compatibility-source" &&
    $5 == "8e1051fe39379367aecf014f41744ce7539a856f" &&
    $6 == "sha256:e089edb1b32df867bf27f3bca224228f49a850a75bfa4f693d293306e83db72c" {
      slug_source++
    }
  $1 == "slug" && $2 == "differential-corpus" &&
    $5 == "138e5806ed7e4e21591948b661119d3810f052cf;spatie-82a69be1ef661ce2ff38242b271457ef0b9611dd" &&
    $6 == "sha256:56cc72e173eed7efb6b6ae6f738bcdde075c4f0e66e54190e89fdff40fbc88e9" {
      slug_corpus++
    }
  END {
    if (NR != 9) exit 1
    if (seen["uuid"] != 1 || seen["ulid"] != 1 || seen["typeid"] != 2 ||
        seen["ksuid"] != 1 || seen["nanoid"] != 1 || seen["slug"] != 2) exit 1
    if (typeid_valid != 1 || typeid_invalid != 1) exit 1
    if (slug_source != 1 || slug_corpus != 1) exit 1
  }
' specification/vector-provenance.tsv
