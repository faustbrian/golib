#!/bin/sh
set -eu

duration=${1:-10s}
workers=${2:-4}
for target in FuzzParseSignatureHeaders FuzzCanonicalize FuzzTimestampVerification FuzzDeliveryWire FuzzEnvelope FuzzSSRFPolicy
do
    go test . -run '^$' -fuzz "^${target}$" -fuzztime "$duration" -parallel "$workers"
done
