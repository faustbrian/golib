package xls

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func TestBIFFWorkbookHeaderConditionsAreIndependent(t *testing.T) {
	t.Parallel()

	valid := syntheticWorkbook(t)
	tests := []struct {
		name string
		data []byte
	}{
		{name: "record header", data: valid[:3]},
		{name: "record identifier", data: append(biffRecord(0x0808, []byte{0, 6, 5, 0}), valid[8:]...)},
		{name: "record payload", data: append(biffRecord(0x0809, []byte{0, 6, 5}), valid[8:]...)},
		{name: "BIFF version", data: append(biffRecord(0x0809, []byte{0, 5, 5, 0}), valid[8:]...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseBIFF8(test.data)
			assertErrorText(t, err, "xls: BIFF8 workbook globals not found")
		})
	}
}

func TestBIFFAndWorksheetEOFBoundaries(t *testing.T) {
	t.Parallel()

	workbook := syntheticWorkbook(t)
	workbook = append(workbook, 1, 2, 3)
	parsed, err := parseBIFF8(workbook)
	if err != nil || len(parsed.Sheets) != 1 {
		t.Fatalf("workbook trailing bytes = %#v, %v", parsed, err)
	}

	worksheet := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	_, err = parseSheet(worksheet, 0, nil)
	assertErrorText(t, err, "xls: truncated BIFF record header")
	_, err = parseSheet(biffRecord(0x0808, []byte{0, 6, 0x10, 0}), 0, nil)
	assertErrorText(t, err, "worksheet BOF not found")
}

func TestBIFFRecordAndStringExactBoundaries(t *testing.T) {
	t.Parallel()

	recordData := biffRecord(0x4321, nil)
	rec, err := readRecord(recordData, 0)
	if err != nil || rec.id != 0x4321 || rec.next != len(recordData) || len(rec.payload) != 0 {
		t.Fatalf("readRecord exact boundary = %#v, %v", rec, err)
	}
	_, err = readRecord(recordData, 1)
	assertErrorText(t, err, "xls: truncated BIFF record header")

	value, used, err := decodeBIFFString(nil, 0, false)
	if err != nil || value != "" || used != 0 {
		t.Fatalf("decodeBIFFString empty = %q, %d, %v", value, used, err)
	}
	value, used, err = decodeBIFFString([]byte{'x'}, 1, false)
	if err != nil || value != "x" || used != 1 {
		t.Fatalf("decodeBIFFString exact compressed = %q, %d, %v", value, used, err)
	}
	value, used, err = decodeBIFFString([]byte{'x', 0}, 1, true)
	if err != nil || value != "x" || used != 2 {
		t.Fatalf("decodeBIFFString exact wide = %q, %d, %v", value, used, err)
	}
}

func TestBoundSheetMinimumRecordIsAccepted(t *testing.T) {
	t.Parallel()

	sheet, err := parseBoundSheet(make([]byte, 8))
	if err != nil || sheet.name != "" || sheet.offset != 0 {
		t.Fatalf("minimum BOUNDSHEET = %#v, %v", sheet, err)
	}
}

func TestSheetRecordLengthBoundaries(t *testing.T) {
	t.Parallel()

	bof := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	eof := biffRecord(0x000a, nil)
	tests := []struct {
		name     string
		record   []byte
		presence bool
		want     string
	}{
		{name: "ROW below", record: biffRecord(0x0208, make([]byte, 5)), want: "truncated ROW record"},
		{name: "LABELSST below", record: biffRecord(0x00fd, make([]byte, 9)), want: "invalid LABELSST record"},
		{name: "LABELSST above", record: biffRecord(0x00fd, make([]byte, 11)), want: "invalid LABELSST record"},
		{name: "NUMBER below", record: biffRecord(0x0203, make([]byte, 13)), want: "invalid NUMBER record"},
		{name: "NUMBER above", record: biffRecord(0x0203, make([]byte, 15)), want: "invalid NUMBER record"},
		{name: "RK below", record: biffRecord(0x027e, make([]byte, 9)), want: "invalid RK record"},
		{name: "RK above", record: biffRecord(0x027e, make([]byte, 11)), want: "invalid RK record"},
		{name: "MULRK below", record: biffRecord(0x00bd, make([]byte, 11)), want: "invalid MULRK record"},
		{name: "MULRK misaligned", record: biffRecord(0x00bd, make([]byte, 13)), want: "invalid MULRK record"},
		{name: "BLANK below", record: biffRecord(0x0201, make([]byte, 5)), presence: true, want: "invalid BLANK record"},
		{name: "BLANK above", record: biffRecord(0x0201, make([]byte, 7)), presence: true, want: "invalid BLANK record"},
		{name: "MULBLANK below", record: biffRecord(0x00be, make([]byte, 7)), presence: true, want: "invalid MULBLANK record"},
		{name: "MULBLANK misaligned", record: biffRecord(0x00be, make([]byte, 9)), presence: true, want: "invalid MULBLANK record"},
		{name: "BOOLERR below", record: biffRecord(0x0205, make([]byte, 7)), want: "invalid BOOLERR record"},
		{name: "BOOLERR above", record: biffRecord(0x0205, make([]byte, 9)), want: "invalid BOOLERR record"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := append(append(append([]byte{}, bof...), test.record...), eof...)
			if test.presence {
				_, _, err := parseSheetWithPresence(data, 0, nil)
				assertErrorText(t, err, test.want)
				return
			}
			_, err := parseSheet(data, 0, nil)
			assertErrorText(t, err, test.want)
		})
	}
}

func TestSheetExactSingleAndMultipleCellRanges(t *testing.T) {
	t.Parallel()

	bof := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	data := append([]byte{}, bof...)
	data = append(data, biffRecord(0x00bd, mulRKPayload(2, 3, []uint32{uint32(7<<2) | 2}))...)
	data = append(data, biffRecord(0x00be, mulBlankPayload(1, 1, 1))...)
	data = append(data, biffRecord(0x000a, nil)...)
	rows, presence, err := parseSheetWithPresence(data, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || len(rows[1]) != 2 || len(rows[2]) != 4 || rows[2][3].Value != "7" {
		t.Fatalf("single-cell ranges = %#v", rows)
	}
	wantPresence := [][]bool{{}, {false, true}, {false, false, false, true}}
	if !reflect.DeepEqual(presence, wantPresence) {
		t.Fatalf("single-cell presence = %#v, want %#v", presence, wantPresence)
	}
}

func TestSheetExactFormattingAndSharedIndexBoundaries(t *testing.T) {
	t.Parallel()

	bof := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	data := append([]byte{}, bof...)
	data = append(data, biffRecord(0x0203, numberPayload(0, 0, 12.25))...)
	data = append(data, biffRecord(0x027e, rkPayload(0, 1, mathFloatRKBits(12.25)))...)
	data = append(data, biffRecord(0x000a, nil)...)
	rows, err := parseSheet(data, 0, nil)
	if err != nil || rows[0][0].Value != "12.25" || rows[0][1].Value != "12.25" {
		t.Fatalf("formatted numeric cells = %#v, %v", rows, err)
	}

	data = append([]byte{}, bof...)
	data = append(data, biffRecord(0x00fd, labelSSTPayload(0, 0, 1))...)
	data = append(data, biffRecord(0x000a, nil)...)
	_, err = parseSheet(data, 0, []string{"only"})
	assertErrorText(t, err, "shared string index outside SST")
}

func TestSSTCountAndSegmentBoundaries(t *testing.T) {
	t.Parallel()

	header := make([]byte, 8)
	strings, err := parseSST([][]byte{header})
	if err != nil || len(strings) != 0 {
		t.Fatalf("empty SST = %#v, %v", strings, err)
	}

	binary.LittleEndian.PutUint32(header[4:], 1)
	strings, err = parseSST([][]byte{append(append([]byte{}, header...), 0, 0, 0)})
	if err != nil || !reflect.DeepEqual(strings, []string{""}) {
		t.Fatalf("minimal SST = %#v, %v", strings, err)
	}
	binary.LittleEndian.PutUint32(header[4:], 2)
	_, err = parseSST([][]byte{append(append([]byte{}, header...), 0, 0, 0)})
	assertErrorText(t, err, "xls: invalid SST string count")
	_, err = parseSST([][]byte{append(append([]byte{}, header...), 1, 0, 0, 'a', 0, 0)})
	assertErrorText(t, err, "xls: truncated SST string")

	reader := &segmentedReader{segments: [][]byte{{1}, {2}}}
	if reader.atBoundary() || reader.remaining() != 2 {
		t.Fatalf("initial segmented state: boundary=%v remaining=%d", reader.atBoundary(), reader.remaining())
	}
	data, err := reader.readRawWithinSegment(1)
	if err != nil || !reflect.DeepEqual(data, []byte{1}) || !reader.atBoundary() || reader.remaining() != 1 {
		t.Fatalf("exact segment read = %v, %v; boundary=%v remaining=%d", data, err, reader.atBoundary(), reader.remaining())
	}
	_, err = reader.readRawWithinSegment(1)
	assertErrorText(t, err, "xls: segment boundary")
	exact := &segmentedReader{segments: [][]byte{{3}}}
	data, err = exact.readRawWithinSegment(1)
	if err != nil || !reflect.DeepEqual(data, []byte{3}) || !exact.atBoundary() {
		t.Fatalf("whole segment read = %v, %v; boundary=%v", data, err, exact.atBoundary())
	}
	exhausted := &segmentedReader{segments: [][]byte{{1}}, segment: 1}
	if exhausted.atBoundary() || exhausted.remaining() != 0 {
		t.Fatalf("exhausted reader: boundary=%v remaining=%d", exhausted.atBoundary(), exhausted.remaining())
	}
	_, err = exhausted.readRawWithinSegment(0)
	assertErrorText(t, err, "xls: segment boundary")
	remaining := (&segmentedReader{segments: [][]byte{{1, 2, 3}, {4, 5}, {6, 7}}, offset: 1}).remaining()
	if remaining != 6 {
		t.Fatalf("remaining across segments = %d", remaining)
	}
	partial := &segmentedReader{segments: [][]byte{{1, 2, 3}, {4, 5}}, offset: 1}
	data, err = partial.readRaw(3)
	if err != nil || !reflect.DeepEqual(data, []byte{2, 3, 4}) || partial.remaining() != 1 {
		t.Fatalf("partial segmented read = %v, %v; remaining=%d", data, err, partial.remaining())
	}
}

func TestCompoundPrimitiveExactBoundaries(t *testing.T) {
	t.Parallel()

	compound := &compoundFile{
		data:           make([]byte, 3*8),
		sectorSize:     8,
		miniSectorSize: 2,
		miniCutoff:     4,
		fat:            []uint32{1, endOfChain},
		miniFat:        []uint32{1, endOfChain},
		miniStream:     []byte{1, 2, 3, 4},
	}
	copy(compound.data[8:16], []byte{9, 9, 9, 9, 9, 9, 9, 9})
	copy(compound.data[16:24], []byte{8, 8, 8, 8, 8, 8, 8, 8})
	if compound.sectorCount() != 2 {
		t.Fatalf("sectorCount = %d", compound.sectorCount())
	}
	sector, err := compound.sector(1)
	if err != nil || len(sector) != 8 {
		t.Fatalf("last sector = %v, %v", sector, err)
	}
	_, err = compound.sector(2)
	assertErrorText(t, err, "xls: sector outside file")

	regular, err := compound.readRegularStream(0, 16)
	if err != nil || len(regular) != 16 {
		t.Fatalf("exact regular stream = %d, %v", len(regular), err)
	}
	mini, err := compound.readMiniStream(0, 4)
	if err != nil || len(mini) != 4 {
		t.Fatalf("exact mini stream = %d, %v", len(mini), err)
	}
	entry, err := compound.readEntry(directory{start: 0, size: 4})
	if err != nil || !reflect.DeepEqual(entry, []byte{9, 9, 9, 9}) {
		t.Fatalf("cutoff-sized entry = %d, %v", len(entry), err)
	}
	entry, err = compound.readEntry(directory{start: 0, size: 3})
	if err != nil || !reflect.DeepEqual(entry, []byte{1, 2, 3}) {
		t.Fatalf("mini entry = %d, %v", len(entry), err)
	}
	_, err = compound.readRegularStream(0, 17)
	assertErrorText(t, err, "xls: stream size exceeds sector chain")
	chainLimited := *compound
	chainLimited.miniStream = make([]byte, 8)
	_, err = chainLimited.readMiniStream(0, 5)
	assertErrorText(t, err, "xls: mini stream size exceeds chain")
	streamLimited := *compound
	streamLimited.miniFat = []uint32{endOfChain}
	streamLimited.miniStream = []byte{1}
	_, err = streamLimited.readMiniStream(0, 2)
	assertErrorText(t, err, "xls: mini stream size exceeds chain")
}

func TestCompoundHeaderAndDirectoryExactBoundaries(t *testing.T) {
	t.Parallel()

	valid := xlsFixture(t)
	_, err := parseCompoundFile(valid[:511])
	assertErrorText(t, err, "xls: invalid OLE2 signature")
	headerOnly := append([]byte(nil), valid[:512]...)
	_, err = parseCompoundFile(headerOnly)
	if err == nil || err.Error() == "xls: invalid OLE2 signature" {
		t.Fatalf("exact header boundary = %v", err)
	}

	wrongPair := mutateUint16(30, 12)(valid)
	_, err = parseCompoundFile(wrongPair)
	assertErrorText(t, err, "xls: unsupported OLE2 version")
	v4WrongShift := mutateUint16(26, 4)(valid)
	_, err = parseCompoundFile(v4WrongShift)
	assertErrorText(t, err, "xls: unsupported OLE2 version")
	v4 := mutateUint16(26, 4)(wrongPair)
	_, err = parseCompoundFile(v4)
	if err == nil || err.Error() == "xls: unsupported OLE2 version" {
		t.Fatalf("OLE2 v4 header was not accepted before structural validation: %v", err)
	}
	shortV4 := append([]byte(nil), v4[:512]...)
	_, err = parseCompoundFile(shortV4)
	assertErrorText(t, err, "xls: truncated OLE2 sectors")
	v4Sector := make([]byte, 4096)
	copy(v4Sector, v4[:512])
	_, err = parseCompoundFile(v4Sector)
	if err == nil || err.Error() == "xls: truncated OLE2 sectors" {
		t.Fatalf("exact v4 sector boundary = %v", err)
	}

	sectorCount := uint32(len(valid)/512 - 1)
	exactFATCount := mutateUint32(44, sectorCount)(valid)
	_, err = parseCompoundFile(exactFATCount)
	if err == nil || err.Error() == "xls: invalid FAT sector count" {
		t.Fatalf("exact FAT count boundary = %v", err)
	}

	rootOffset := directoryEntryOffset(t, valid, "Root Entry")
	emptyName := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(emptyName[rootOffset+64:rootOffset+66], 2)
	if _, err = parseCompoundFile(emptyName); err != nil {
		t.Fatalf("minimum directory name length: %v", err)
	}
	maximumName := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(maximumName[rootOffset+64:rootOffset+66], 64)
	if _, err = parseCompoundFile(maximumName); err != nil {
		t.Fatalf("maximum directory name length: %v", err)
	}
	aboveMaximum := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(aboveMaximum[rootOffset+64:rootOffset+66], 66)
	_, err = parseCompoundFile(aboveMaximum)
	assertErrorText(t, err, "xls: invalid directory name")

	ignored := append([]byte(nil), valid...)
	baseCompound, err := parseCompoundFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	ignoredOffset := -1
	for _, entry := range baseCompound.directories {
		if entry.typeID != 5 && entry.name != "Workbook" && entry.name != "Book" {
			ignoredOffset = directoryEntryOffset(t, ignored, entry.name)
			break
		}
	}
	if ignoredOffset < 0 {
		t.Fatal("fixture has no non-essential directory entry")
	}
	ignored[ignoredOffset+66] = 0
	binary.LittleEndian.PutUint16(ignored[ignoredOffset+64:ignoredOffset+66], 1)
	if _, err = parseCompoundFile(ignored); err != nil {
		t.Fatalf("empty directory entry was not ignored: %v", err)
	}

	highSize := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(highSize[rootOffset+124:rootOffset+128], math.MaxUint32)
	if _, err = parseCompoundFile(highSize); err != nil {
		t.Fatalf("OLE2 v3 high size bits were not ignored: %v", err)
	}

	zeroRoot := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint64(zeroRoot[rootOffset+120:rootOffset+128], 0)
	compound, err := parseCompoundFile(zeroRoot)
	if err != nil || len(compound.miniStream) != 0 {
		t.Fatalf("zero-sized root stream = %d, %v", len(compound.miniStream), err)
	}
	withoutMiniFAT := mutateUint32(64, 0)(zeroRoot)
	compound, err = parseCompoundFile(withoutMiniFAT)
	if err != nil || len(compound.miniFat) != 0 {
		t.Fatalf("zero mini FAT count = %d, %v", len(compound.miniFat), err)
	}

	compound, err = parseCompoundFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	wantMiniFATEntries := int(binary.LittleEndian.Uint32(valid[64:68])) * 512 / 4
	if len(compound.miniFat) != wantMiniFATEntries {
		t.Fatalf("mini FAT entries = %d, want %d", len(compound.miniFat), wantMiniFATEntries)
	}
}

func TestCompoundChainConditionsAreIndependent(t *testing.T) {
	t.Parallel()

	compound := &compoundFile{}
	tests := []struct {
		name  string
		start uint32
		table []uint32
		limit int
		want  string
	}{
		{name: "free", start: freeSector, table: []uint32{endOfChain}, limit: 1, want: "xls: invalid sector chain"},
		{name: "outside", start: 1, table: []uint32{endOfChain}, limit: 2, want: "xls: invalid sector chain"},
		{name: "limit", start: 0, table: []uint32{1, endOfChain}, limit: 1, want: "xls: invalid sector chain"},
		{name: "cycle", start: 0, table: []uint32{0}, limit: 2, want: "xls: cyclic sector chain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compound.chain(test.start, test.table, test.limit)
			assertErrorText(t, err, test.want)
		})
	}
}

func TestDIFATReaderHandlesHeaderAndExtendedSectors(t *testing.T) {
	t.Parallel()

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], 3)
	binary.LittleEndian.PutUint32(header[4:8], freeSector)
	compound := &compoundFile{data: make([]byte, 3*16), sectorSize: 16}
	sector := compound.data[16:32]
	binary.LittleEndian.PutUint32(sector[0:4], 4)
	binary.LittleEndian.PutUint32(sector[4:8], freeSector)
	binary.LittleEndian.PutUint32(sector[8:12], 5)
	binary.LittleEndian.PutUint32(sector[12:16], endOfChain)

	difat, err := readDIFAT(compound, header, 0, 1, 3)
	if err != nil || !reflect.DeepEqual(difat, []uint32{3, 4, 5}) {
		t.Fatalf("extended DIFAT = %v, %v", difat, err)
	}
	if got := streamSize(2, 512); got != 1024 {
		t.Fatalf("streamSize = %d", got)
	}
	difat, err = readDIFAT(compound, header, endOfChain, 0, 1)
	if err != nil || !reflect.DeepEqual(difat, []uint32{3}) {
		t.Fatalf("header DIFAT = %v, %v", difat, err)
	}
	extraHeader := append([]byte(nil), header...)
	binary.LittleEndian.PutUint32(extraHeader[4:8], 4)
	difat, err = readDIFAT(compound, extraHeader, endOfChain, 0, 1)
	if err != nil || !reflect.DeepEqual(difat, []uint32{3}) {
		t.Fatalf("bounded header DIFAT = %v, %v", difat, err)
	}

	tests := []struct {
		name   string
		start  uint32
		count  uint32
		wanted uint32
		mutate func([]byte)
		want   string
	}{
		{name: "truncated end", start: endOfChain, count: 1, wanted: 2, want: "xls: truncated DIFAT chain"},
		{name: "truncated free", start: freeSector, count: 1, wanted: 2, want: "xls: truncated DIFAT chain"},
		{name: "outside", start: 2, count: 1, wanted: 2, want: "xls: sector outside file"},
		{name: "cycle", start: 0, count: 2, wanted: 2, mutate: func(data []byte) { binary.LittleEndian.PutUint32(data[12:16], 0) }, want: "xls: cyclic DIFAT chain"},
		{name: "incomplete", start: 0, count: 1, wanted: 4, want: "xls: incomplete FAT index"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			local := *compound
			local.data = append([]byte(nil), compound.data...)
			if test.mutate != nil {
				test.mutate(local.data[16:32])
			}
			_, err := readDIFAT(&local, header, test.start, test.count, test.wanted)
			assertErrorText(t, err, test.want)
		})
	}
}

func TestDirectoryParserAndRootLookupBoundaries(t *testing.T) {
	t.Parallel()

	entries := make([]byte, 3*128)
	entries[66] = 0
	writeDirectory := func(entry []byte, name string, typeID byte, size uint64) {
		encoded := encodeWide(name + "\x00")
		copy(entry, encoded)
		binary.LittleEndian.PutUint16(entry[64:66], uint16(len(encoded)))
		entry[66] = typeID
		binary.LittleEndian.PutUint64(entry[120:128], size)
	}
	writeDirectory(entries[128:256], "Root Entry", 5, uint64(1)<<32|7)
	writeDirectory(entries[256:384], "Workbook", 2, 9)
	directories, err := parseDirectories(entries, 3)
	if err != nil || len(directories) != 2 || directories[0].size != 7 || directories[1].name != "Workbook" {
		t.Fatalf("directories = %#v, %v", directories, err)
	}
	root := findRoot(directories)
	if root == nil || root.name != "Root Entry" {
		t.Fatalf("root = %#v", root)
	}
	if findRoot([]directory{{typeID: 2}}) != nil {
		t.Fatal("findRoot accepted non-root entry")
	}
	directories, err = parseDirectories(entries, 4)
	if err != nil || directories[0].size != (uint64(1)<<32|7) {
		t.Fatalf("v4 directory size = %#v, %v", directories, err)
	}

	for _, nameBytes := range []uint16{0, 1, 65, 66} {
		invalid := append([]byte(nil), entries[128:256]...)
		binary.LittleEndian.PutUint16(invalid[64:66], nameBytes)
		_, err = parseDirectories(invalid, 3)
		assertErrorText(t, err, "xls: invalid directory name")
	}
}

func assertErrorText(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func mathFloatRKBits(value float64) uint32 {
	return uint32(math.Float64bits(value) >> 32)
}
