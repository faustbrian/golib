#!/usr/bin/env bash
set -euo pipefail

go test . -count=1 -run \
  '^(TestParseEntryBoundaries|TestParseRejectsDTDWithoutResolvingEntities|TestMarshalWithOptionsEnforcesResourceLimits)$'
go test ./compile -count=1 -run \
  '^(TestCompileResolvesCyclesOnceAndAppliesChameleonNamespace|TestCompileUsesDenyResolverByDefault|TestCompileBoundsSchemaCount|TestValidateModelGroupEnforcesParticleLimitRecursively)$'
go test ./datatype -count=1 -run \
  '^(TestCompilePatternBoundsTranslationWork|TestCompilePatternRejectsInvalidExpressions)$'
go test ./resolve -count=1 -run \
  '^(TestFileResolverReadsOnlyWithinItsConfiguredRoot|TestFileResolverRejectsUnsafeAndOversizedResources|TestFileResolverRejectsSymlinkEscape|TestDenyResolverRejectsEveryResource)$'
go test ./validate -count=1 -run \
  '^(TestParseInstanceEnforcesEveryResourceBoundary|TestValidateAttributesStopsWhenDiagnosticsLimitIsReached|TestValidateIdentityConstraintsStopsAtEveryLimitBoundary|TestValidateIdentityConstraintsStopsAtEveryDiagnosticBoundary)$'
