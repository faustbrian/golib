#!/bin/sh
set -eu

manifest=specification/tooling.tsv
image='eclipse-temurin:25-jdk@sha256:201fbb8886b2d273218aa3a192f0afbf7b5ff65ee8cc6ef47f5dce2171f013ea'
workdir=$(mktemp -d "$(pwd)/.woden.XXXXXX")
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

command -v curl >/dev/null
command -v docker >/dev/null || {
    echo "docker is required for the pinned Java interoperability runtime" >&2
    exit 1
}

tail -n +2 "$manifest" | while IFS="$(printf '\t')" read -r id version license digest bytes url; do
    destination="$workdir/$id.jar"
    curl --fail --location --silent --show-error --output "$destination" "$url"
    actual_digest=$(shasum -a 256 "$destination" | awk '{print $1}')
    actual_bytes=$(wc -c < "$destination" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done

docker run --rm \
    --volume "$workdir:/work" \
    --volume "$(pwd):/repo:ro" \
    --workdir /repo \
    "$image" \
    sh -eu -c '
        classpath=/work/apache-woden-core.jar
        classpath="$classpath:/work/apache-xmlschema-core.jar"
        classpath="$classpath:/work/apache-commons-logging.jar"
        javac -cp "$classpath" -d /work \
            testdata/interoperability/woden/WodenProbe.java
        java -cp "/work:$classpath" WodenProbe \
            /repo/testdata/w3c/wsdl20/IRI-1G.wsdl \
            /repo/testdata/w3c/wsdl20/Multipart-1G.wsdl \
            > /work/actual.tsv
    '

tail -n +2 testdata/interoperability/woden/expected.tsv > "$workdir/expected.tsv"
diff -u "$workdir/expected.tsv" "$workdir/actual.tsv"
