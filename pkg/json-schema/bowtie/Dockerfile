FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' \
    -o /out/bowtie-json-schema ./cmd/bowtie-json-schema

FROM scratch
COPY --from=build /out/bowtie-json-schema /bowtie-json-schema
USER 65532:65532
ENTRYPOINT ["/bowtie-json-schema"]
