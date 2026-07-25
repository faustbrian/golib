package tabular

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestZIPArchiveListsAndExtractsFixtureEntries(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/archive/import.zip")
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, file)
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}

	archive, err := OpenZIP(file, info.Size(), ZIPConfig{})
	if err != nil {
		t.Fatalf("OpenZIP() error = %v", err)
	}
	wantEntries := []ZIPEntry{
		{Name: "orders.csv", UncompressedSize: 27},
		{Name: "nested/", Directory: true},
		{Name: "nested/readme.txt", UncompressedSize: 15},
	}
	if got := archive.Entries(); !reflect.DeepEqual(got, wantEntries) {
		t.Fatalf("Entries() = %#v, want %#v", got, wantEntries)
	}

	entry, err := archive.Open("orders.csv")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, readErr := io.ReadAll(entry)
	closeErr := entry.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read error = %v, close error = %v", readErr, closeErr)
	}
	if string(data) != "id;amount\n1;12,50\n2;100,00\n" {
		t.Fatalf("entry data = %q", data)
	}

	var destination bytes.Buffer
	if err = archive.Extract("nested/readme.txt", &destination); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if destination.String() != "tabular import\n" {
		t.Fatalf("extracted data = %q", destination.String())
	}
}

func TestZIPArchiveReturnsEntriesCopy(t *testing.T) {
	t.Parallel()

	data := makeZIP(t, map[string]string{"data.csv": "a,b\n1,2\n"})
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	entries := archive.Entries()
	entries[0].Name = "changed"
	if archive.Entries()[0].Name != "data.csv" {
		t.Fatal("Entries() returned mutable internal state")
	}
}

func TestOpenZIPRejectsBrokenArchives(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/archive/broken.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if !errors.Is(err, ErrorArchive) {
		t.Fatalf("OpenZIP() error = %v, want archive kind", err)
	}
}

func TestOpenZIPValidatesConfigurationAndArchiveLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   []byte
		size   int64
		config ZIPConfig
		kind   ErrorKind
	}{
		{name: "nil source", size: 0, config: ZIPConfig{}, kind: ErrorInvalidConfig},
		{name: "negative size", data: []byte("x"), size: -1, config: ZIPConfig{}, kind: ErrorInvalidConfig},
		{name: "negative max entries", data: []byte("x"), size: 1, config: ZIPConfig{MaxEntries: -1}, kind: ErrorInvalidConfig},
		{name: "entry count", data: makeZIP(t, map[string]string{"a": "1", "b": "2"}), config: ZIPConfig{MaxEntries: 1}, kind: ErrorLimitExceeded},
		{name: "entry size", data: makeZIP(t, map[string]string{"a": "1234"}), config: ZIPConfig{MaxEntryBytes: 3}, kind: ErrorLimitExceeded},
		{name: "total size", data: makeZIP(t, map[string]string{"a": "12", "b": "34"}), config: ZIPConfig{MaxTotalBytes: 3}, kind: ErrorLimitExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var source io.ReaderAt
			if test.data != nil {
				source = bytes.NewReader(test.data)
				if test.size == 0 {
					test.size = int64(len(test.data))
				}
			}
			_, err := OpenZIP(source, test.size, test.config)
			if !errors.Is(err, test.kind) {
				t.Fatalf("OpenZIP() error = %v, want kind %v", err, test.kind)
			}
		})
	}
}

func TestZIPEntryReadStopsAtDeclaredSize(t *testing.T) {
	t.Parallel()

	data := makeZIP(t, map[string]string{"data.csv": strings.Repeat("x", 128*1024)})
	setZIPDeclaredSize(t, data, "data.csv", 1)
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{
		MaxEntryBytes: 1024,
		MaxTotalBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := archive.Open("data.csv")
	if err != nil {
		t.Fatal(err)
	}
	contents, readErr := io.ReadAll(entry)
	_ = entry.Close()
	if len(contents) > 1 {
		t.Fatalf("read %d bytes from an entry declaring 1 byte", len(contents))
	}
	if !errors.Is(readErr, ErrorArchive) {
		t.Fatalf("Read() error = %v, want archive kind", readErr)
	}
}

func TestOpenZIPRejectsUnsafeAndDuplicateNames(t *testing.T) {
	t.Parallel()

	for _, names := range [][]string{{"../escape.csv"}, {"/absolute.csv"}, {"same.csv", "same.csv"}} {
		data := makeZIPEntries(t, names)
		_, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
		if !errors.Is(err, ErrorArchive) {
			t.Fatalf("OpenZIP(%v) error = %v, want archive kind", names, err)
		}
	}
}

func TestZIPArchiveReportsMissingAndDirectoryTargets(t *testing.T) {
	t.Parallel()

	data := makeZIPEntries(t, []string{"folder/", "folder/data.csv"})
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = archive.Open("missing.csv"); !errors.Is(err, ErrorEntryNotFound) {
		t.Fatalf("Open(missing) error = %v, want entry-not-found kind", err)
	}
	if _, err = archive.Open("folder/"); !errors.Is(err, ErrorArchive) {
		t.Fatalf("Open(directory) error = %v, want archive kind", err)
	}
	if err = archive.Extract("folder/data.csv", nil); !errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("Extract(nil) error = %v, want invalid-config kind", err)
	}
}

func TestZIPArchiveSurfacesDestinationWriteFailures(t *testing.T) {
	t.Parallel()

	data := makeZIP(t, map[string]string{"data.csv": "a,b\n"})
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("write failed")
	err = archive.Extract("data.csv", errorWriter{err: want})
	if !errors.Is(err, ErrorArchive) || !errors.Is(err, want) {
		t.Fatalf("Extract() error = %v, want archive kind wrapping writer error", err)
	}
}

func FuzzOpenZIP(f *testing.F) {
	seed := makeZIP(f, map[string]string{"data.csv": "a,b\n1,2\n"})
	f.Add(seed)
	f.Fuzz(func(_ *testing.T, data []byte) {
		archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{
			MaxEntries:    10,
			MaxEntryBytes: 1024,
			MaxTotalBytes: 2048,
		})
		if err != nil {
			return
		}
		for _, entry := range archive.Entries() {
			if entry.Directory {
				continue
			}
			reader, err := archive.Open(entry.Name)
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, reader)
			_ = reader.Close()
		}
	})
}

func BenchmarkZIPExtract(b *testing.B) {
	data := makeZIP(b, map[string]string{"data.csv": strings.Repeat("1,Alice,Helsinki\n", 20_000)})
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		if err = archive.Extract("data.csv", io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

type testingTB interface {
	Helper()
	Fatal(args ...any)
}

func makeZIP(t testingTB, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(entry, files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeZIPEntries(t testingTB, names []string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			if _, err = io.WriteString(entry, "data"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func setZIPDeclaredSize(t *testing.T, data []byte, target string, size uint32) {
	t.Helper()
	for offset := bytes.Index(data, []byte{'P', 'K', 1, 2}); offset >= 0 && offset+46 <= len(data); {
		nameLength := int(binary.LittleEndian.Uint16(data[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(data[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(data[offset+32 : offset+34]))
		nameStart := offset + 46
		if string(data[nameStart:nameStart+nameLength]) == target {
			binary.LittleEndian.PutUint32(data[offset+24:offset+28], size)
			return
		}
		offset = nameStart + nameLength + extraLength + commentLength
		if offset+4 > len(data) || !bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			break
		}
	}
	t.Fatalf("ZIP entry %q not found", target)
}

type errorWriter struct{ err error }

func (writer errorWriter) Write([]byte) (int, error) { return 0, writer.err }
