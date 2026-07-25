//go:build largebench

package tabular

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	largeBenchmarkBytes = 50 * 1024 * 1024
	largeBenchmarkRows  = 100_000
)

func BenchmarkDelimitedReader50MiB(b *testing.B) {
	path, size := writeLargeCSVBenchmark(b)
	b.Cleanup(func() { _ = os.Remove(path) })
	b.ReportAllocs()
	b.SetBytes(size)

	var elapsed time.Duration
	b.ResetTimer()
	for range b.N {
		started := time.Now()
		file, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		reader, err := NewCSVReader(file, DelimitedConfig{})
		if err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
		rows, err := consumeBenchmarkRows(reader)
		closeErr := file.Close()
		elapsed += time.Since(started)
		if err != nil || closeErr != nil {
			b.Fatalf("consume CSV: %v; close: %v", err, closeErr)
		}
		if rows != largeBenchmarkRows {
			b.Fatalf("row count = %d, want %d", rows, largeBenchmarkRows)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(size)/(1024*1024), "input-MiB")
	b.ReportMetric(largeBenchmarkRows, "rows/op")
	b.ReportMetric(float64(largeBenchmarkRows*b.N)/elapsed.Seconds(), "rows/s")
}

func BenchmarkXLSXReader50MiB(b *testing.B) {
	path, size := writeLargeXLSXBenchmark(b)
	b.Cleanup(func() { _ = os.Remove(path) })
	b.ReportAllocs()
	b.SetBytes(size)

	var elapsed time.Duration
	b.ResetTimer()
	for range b.N {
		started := time.Now()
		file, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{
			Format: FormatXLSX,
			Header: &HeaderConfig{RejectEmpty: true, RejectDuplicates: true},
		})
		if err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
		rows, err := consumeBenchmarkRows(reader)
		readerCloseErr := reader.Close()
		fileCloseErr := file.Close()
		elapsed += time.Since(started)
		if err != nil || readerCloseErr != nil || fileCloseErr != nil {
			b.Fatalf("consume XLSX: %v; reader close: %v; file close: %v", err, readerCloseErr, fileCloseErr)
		}
		if rows != largeBenchmarkRows {
			b.Fatalf("row count = %d, want %d", rows, largeBenchmarkRows)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(size)/(1024*1024), "input-MiB")
	b.ReportMetric(largeBenchmarkRows, "rows/op")
	b.ReportMetric(float64(largeBenchmarkRows*b.N)/elapsed.Seconds(), "rows/s")
}

func writeLargeCSVBenchmark(b *testing.B) (string, int64) {
	b.Helper()
	file, err := os.CreateTemp(b.TempDir(), "tabular-*.csv")
	if err != nil {
		b.Fatal(err)
	}
	for row := 0; row < largeBenchmarkRows; row++ {
		if _, err = fmt.Fprintf(file, "%d,%s,%s,%s,%s,%s\n", row,
			largeBenchmarkField(row, 0), largeBenchmarkField(row, 1),
			largeBenchmarkField(row, 2), largeBenchmarkField(row, 3),
			largeBenchmarkField(row, 4)); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
	}
	return closeLargeBenchmarkFile(b, file)
}

func writeLargeXLSXBenchmark(b *testing.B) (string, int64) {
	b.Helper()
	file, err := os.CreateTemp(b.TempDir(), "tabular-*.xlsx")
	if err != nil {
		b.Fatal(err)
	}
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	stream, err := workbook.NewStreamWriter(sheet)
	if err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	header := make([]any, 12)
	for column := range header {
		header[column] = fmt.Sprintf("column_%02d", column+1)
	}
	if err = stream.SetRow("A1", header); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	for row := 0; row < largeBenchmarkRows; row++ {
		values := make([]any, len(header))
		for column := range values {
			values[column] = largeBenchmarkField(row, column)
		}
		cell, coordinateErr := excelize.CoordinatesToCellName(1, row+2)
		if coordinateErr != nil {
			_ = file.Close()
			b.Fatal(coordinateErr)
		}
		if err = stream.SetRow(cell, values); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
	}
	if err = stream.Flush(); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	if err = workbook.Write(file); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	if err = workbook.Close(); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	return closeLargeBenchmarkFile(b, file)
}

func closeLargeBenchmarkFile(b *testing.B, file *os.File) (string, int64) {
	b.Helper()
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	if err = file.Close(); err != nil {
		b.Fatal(err)
	}
	if info.Size() < largeBenchmarkBytes {
		b.Fatalf("generated %s is %.2f MiB; want at least 50 MiB", info.Name(), float64(info.Size())/(1024*1024))
	}
	return file.Name(), info.Size()
}

func largeBenchmarkField(row, column int) string {
	var seed [16]byte
	binary.LittleEndian.PutUint64(seed[:8], uint64(row))
	binary.LittleEndian.PutUint64(seed[8:], uint64(column))
	first := sha256.Sum256(seed[:])
	seed[15] ^= 0xff
	second := sha256.Sum256(seed[:])
	encoded := make([]byte, hex.EncodedLen(len(first)+len(second)))
	hex.Encode(encoded[:64], first[:])
	hex.Encode(encoded[64:], second[:])
	return string(encoded)
}

func consumeBenchmarkRows(reader interface{ Read() (Row, error) }) (int, error) {
	rows := 0
	for {
		_, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return rows, err
		}
		rows++
	}
}
