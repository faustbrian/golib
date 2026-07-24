//go:build ignore

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 5 {
		return fmt.Errorf("usage: rewrite-archive input.tar output.tar.gz source-root target-root")
	}
	input, err := os.Open(os.Args[1])
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer input.Close()

	output, err := os.OpenFile(os.Args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create release archive: %w", err)
	}
	failed := true
	defer func() {
		_ = output.Close()
		if failed {
			_ = os.Remove(os.Args[2])
		}
	}()

	compressed, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create compressor: %w", err)
	}
	compressed.Header.ModTime = time.Unix(0, 0)
	compressed.Header.OS = 255
	archive := tar.NewWriter(compressed)
	reader := tar.NewReader(input)

	for {
		header, readErr := reader.Next()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read source archive: %w", readErr)
		}
		header.Name = rewriteName(header.Name, os.Args[3], os.Args[4])
		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("write release header: %w", err)
		}
		if _, err := io.Copy(archive, reader); err != nil {
			return fmt.Errorf("write release content: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close release archive: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("close compressor: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close release file: %w", err)
	}
	failed = false

	return nil
}

func rewriteName(name, source, target string) string {
	if name == source {
		return target
	}
	if suffix, ok := strings.CutPrefix(name, source+"/"); ok {
		return target + "/" + suffix
	}
	return name
}
