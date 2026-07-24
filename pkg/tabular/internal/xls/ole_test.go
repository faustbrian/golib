package xls

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

func TestParseCompoundFileReadsRegularAndMiniStreams(t *testing.T) {
	t.Parallel()

	data := xlsFixture(t)
	compound, err := parseCompoundFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if compound.sectorSize != 512 || compound.miniSectorSize != 64 || len(compound.directories) == 0 {
		t.Fatalf("compound metadata = %#v", compound)
	}

	var workbook, small *directory
	for index := range compound.directories {
		entry := &compound.directories[index]
		if entry.name == "Workbook" {
			workbook = entry
		}
		if entry.typeID == 2 && entry.size > 0 && entry.size < compound.miniCutoff {
			small = entry
		}
	}
	if workbook == nil || small == nil {
		t.Fatalf("workbook = %#v, small = %#v", workbook, small)
	}
	regular, err := compound.readEntry(*workbook)
	if err != nil || len(regular) != int(workbook.size) {
		t.Fatalf("read regular entry = %d bytes, %v", len(regular), err)
	}
	mini, err := compound.readEntry(*small)
	if err != nil || len(mini) != int(small.size) {
		t.Fatalf("read mini entry = %d bytes, %v", len(mini), err)
	}
	opened, err := openWorkbookStream(data)
	if err != nil || !bytes.Equal(opened, regular) {
		t.Fatalf("openWorkbookStream() = %d bytes, %v", len(opened), err)
	}
}

func TestParseCompoundFileRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	valid := xlsFixture(t)
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short", mutate: func([]byte) []byte { return []byte("short") }},
		{name: "signature", mutate: mutateAt(0, []byte("BAD!"))},
		{name: "byte order", mutate: mutateUint16(28, 0)},
		{name: "major version", mutate: mutateUint16(26, 2)},
		{name: "sector shift", mutate: mutateUint16(30, 10)},
		{name: "mini shift", mutate: mutateUint16(32, 5)},
		{name: "truncated sector", mutate: func(data []byte) []byte { return data[:len(data)-1] }},
		{name: "fat count", mutate: mutateUint32(44, ^uint32(0))},
		{name: "fat sector", mutate: mutateUint32(76, ^uint32(10))},
		{name: "incomplete fat", mutate: mutateUint32(44, 2)},
		{name: "truncated difat", mutate: func(data []byte) []byte {
			data = mutateUint32(72, 1)(data)
			return mutateUint32(68, endOfChain)(data)
		}},
		{name: "directory sector", mutate: mutateUint32(48, ^uint32(10))},
		{name: "mini fat sector", mutate: mutateUint32(60, ^uint32(10))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseCompoundFile(test.mutate(valid)); err == nil {
				t.Fatal("parseCompoundFile() returned nil error")
			}
		})
	}
}

func TestParseCompoundFileRejectsExtendedDIFATCorruption(t *testing.T) {
	t.Parallel()

	valid := xlsFixture(t)
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "outside sector", mutate: func(data []byte) []byte {
			data = mutateUint32(72, 1)(data)
			return mutateUint32(68, ^uint32(10))(data)
		}},
		{name: "cycle", mutate: func(data []byte) []byte {
			mutated := append([]byte(nil), data...)
			binary.LittleEndian.PutUint32(mutated[44:48], 2)
			binary.LittleEndian.PutUint32(mutated[68:72], 0)
			binary.LittleEndian.PutUint32(mutated[72:76], 2)
			binary.LittleEndian.PutUint32(mutated[512:516], 0)
			binary.LittleEndian.PutUint32(mutated[512+508:512+512], 0)
			return mutated
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseCompoundFile(test.mutate(valid)); err == nil {
				t.Fatal("parseCompoundFile() returned nil error")
			}
		})
	}
}

func TestParseCompoundFileRejectsDirectoryCorruption(t *testing.T) {
	t.Parallel()

	valid := xlsFixture(t)
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "invalid name length", mutate: func(data []byte) []byte {
			mutated := append([]byte(nil), data...)
			offset := directoryEntryOffset(t, mutated, "Root Entry")
			binary.LittleEndian.PutUint16(mutated[offset+64:offset+66], 1)
			return mutated
		}},
		{name: "missing root", mutate: func(data []byte) []byte {
			mutated := append([]byte(nil), data...)
			offset := directoryEntryOffset(t, mutated, "Root Entry")
			mutated[offset+66] = 0
			return mutated
		}},
		{name: "broken root stream", mutate: func(data []byte) []byte {
			mutated := append([]byte(nil), data...)
			offset := directoryEntryOffset(t, mutated, "Root Entry")
			binary.LittleEndian.PutUint32(mutated[offset+116:offset+120], ^uint32(10))
			return mutated
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseCompoundFile(test.mutate(valid)); err == nil {
				t.Fatal("parseCompoundFile() returned nil error")
			}
		})
	}
}

func TestOpenWorkbookStreamRequiresWorkbookDirectory(t *testing.T) {
	t.Parallel()

	data := append([]byte(nil), xlsFixture(t)...)
	offset := directoryEntryOffset(t, data, "Workbook")
	data[offset] = 'X'
	if _, err := openWorkbookStream(data); err == nil {
		t.Fatal("openWorkbookStream() returned nil error")
	}
	if _, err := openWorkbookStream([]byte("broken")); err == nil {
		t.Fatal("openWorkbookStream(broken) returned nil error")
	}
}

func TestCompoundSectorAndChainsFailClosed(t *testing.T) {
	t.Parallel()

	compound := &compoundFile{data: make([]byte, 4*512), sectorSize: 512}
	if compound.sectorCount() != 3 {
		t.Fatalf("sectorCount() = %d", compound.sectorCount())
	}
	sector, err := compound.sector(1)
	if err != nil || len(sector) != 512 {
		t.Fatalf("sector() = %d bytes, %v", len(sector), err)
	}
	if _, err = compound.sector(3); err == nil {
		t.Fatal("sector() accepted outside ID")
	}

	chain, err := compound.chain(0, []uint32{1, endOfChain}, 2)
	if err != nil || !reflect.DeepEqual(chain, []uint32{0, 1}) {
		t.Fatalf("chain() = %v, %v", chain, err)
	}
	for _, test := range []struct {
		start uint32
		table []uint32
		max   int
	}{
		{start: freeSector, table: []uint32{endOfChain}, max: 1},
		{start: 2, table: []uint32{endOfChain}, max: 1},
		{start: 0, table: []uint32{0}, max: 2},
		{start: 0, table: []uint32{1, endOfChain}, max: 1},
	} {
		if _, err = compound.chain(test.start, test.table, test.max); err == nil {
			t.Fatalf("chain(%v) returned nil error", test)
		}
	}
	chain, err = compound.chain(endOfChain, nil, 0)
	if err != nil || chain != nil {
		t.Fatalf("empty chain = %v, %v", chain, err)
	}
}

func TestCompoundStreamReadersValidateSizesAndOffsets(t *testing.T) {
	t.Parallel()

	data := make([]byte, 3*512)
	for index := 512; index < len(data); index++ {
		data[index] = byte(index)
	}
	compound := &compoundFile{
		data:           data,
		sectorSize:     512,
		miniSectorSize: 64,
		fat:            []uint32{1, endOfChain},
	}
	stream, err := compound.readRegularStream(0, 600)
	if err != nil || len(stream) != 600 {
		t.Fatalf("readRegularStream() = %d, %v", len(stream), err)
	}
	whole, err := compound.readRegularStream(0, 0)
	if err != nil || len(whole) != 1024 {
		t.Fatalf("readRegularStream(size zero) = %d, %v", len(whole), err)
	}
	if _, err = compound.readRegularStream(0, 1025); err == nil {
		t.Fatal("readRegularStream accepted oversized stream")
	}
	if _, err = compound.readRegularStream(3, 1); err == nil {
		t.Fatal("readRegularStream accepted invalid chain")
	}

	compound.miniFat = []uint32{1, endOfChain}
	compound.miniStream = make([]byte, 128)
	mini, err := compound.readMiniStream(0, 100)
	if err != nil || len(mini) != 100 {
		t.Fatalf("readMiniStream() = %d, %v", len(mini), err)
	}
	if _, err = compound.readMiniStream(0, 129); err == nil {
		t.Fatal("readMiniStream accepted oversized stream")
	}
	compound.miniStream = compound.miniStream[:64]
	if _, err = compound.readMiniStream(0, 100); err == nil {
		t.Fatal("readMiniStream accepted sector outside root stream")
	}
	compound.miniFat = []uint32{endOfChain, endOfChain, endOfChain}
	compound.miniStream = make([]byte, 128)
	if _, err = compound.readMiniStream(2, 64); err == nil {
		t.Fatal("readMiniStream accepted an outside mini sector")
	}
	if _, err = compound.readMiniStream(3, 1); err == nil {
		t.Fatal("readMiniStream accepted an invalid chain")
	}
}

func xlsFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/spreadsheet/table.xls")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func directoryEntryOffset(t *testing.T, data []byte, name string) int {
	t.Helper()
	encoded := make([]byte, 0, len(name)*2)
	for _, character := range name {
		encoded = binary.LittleEndian.AppendUint16(encoded, uint16(character))
	}
	offset := bytes.Index(data, encoded)
	if offset < 0 {
		t.Fatalf("directory %q not found", name)
	}
	return offset
}

func mutateAt(offset int, replacement []byte) func([]byte) []byte {
	return func(data []byte) []byte {
		mutated := append([]byte(nil), data...)
		mutated = append(mutated[:offset], append(replacement, mutated[offset+len(replacement):]...)...)
		return mutated
	}
}

func mutateUint16(offset int, value uint16) func([]byte) []byte {
	return func(data []byte) []byte {
		mutated := append([]byte(nil), data...)
		binary.LittleEndian.PutUint16(mutated[offset:offset+2], value)
		return mutated
	}
}

func mutateUint32(offset int, value uint32) func([]byte) []byte {
	return func(data []byte) []byte {
		mutated := append([]byte(nil), data...)
		binary.LittleEndian.PutUint32(mutated[offset:offset+4], value)
		return mutated
	}
}
