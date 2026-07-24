package xls

import (
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestReadRecordValidatesBoundaries(t *testing.T) {
	t.Parallel()

	data := biffRecord(0x1234, []byte{1, 2})
	record, err := readRecord(data, 0)
	if err != nil || record.id != 0x1234 || !reflect.DeepEqual(record.payload, []byte{1, 2}) || record.next != 6 {
		t.Fatalf("readRecord() = %#v, %v", record, err)
	}
	for _, test := range []struct {
		data   []byte
		offset int
	}{
		{data: data, offset: -1},
		{data: []byte{1, 2, 3}, offset: 0},
		{data: []byte{1, 0, 2, 0, 1}, offset: 0},
	} {
		if _, err = readRecord(test.data, test.offset); err == nil {
			t.Fatalf("readRecord(%v, %d) returned nil error", test.data, test.offset)
		}
	}
}

func TestParseBoundSheetSupportsCompressedAndWideNames(t *testing.T) {
	t.Parallel()

	compressed := []byte{12, 0, 0, 0, 0, 0, 5, 0, 'O', 'r', 'd', 'e', 'r'}
	sheet, err := parseBoundSheet(compressed)
	if err != nil || sheet.name != "Order" || sheet.offset != 12 {
		t.Fatalf("parseBoundSheet() = %#v, %v", sheet, err)
	}
	wideName := encodeWide("Åbo")
	wide := append([]byte{8, 0, 0, 0, 0, 0, 3, 1}, wideName...)
	sheet, err = parseBoundSheet(wide)
	if err != nil || sheet.name != "Åbo" {
		t.Fatalf("parseBoundSheet(wide) = %#v, %v", sheet, err)
	}
	for _, data := range [][]byte{{1}, {0, 0, 0, 0, 0, 0, 2, 0, 'a'}, append(compressed, 'x')} {
		if _, err = parseBoundSheet(data); err == nil {
			t.Fatalf("parseBoundSheet(%v) returned nil error", data)
		}
	}
}

func TestDecodeBIFFStringValidatesWidths(t *testing.T) {
	t.Parallel()

	value, used, err := decodeBIFFString([]byte("abc"), 3, false)
	if err != nil || value != "abc" || used != 3 {
		t.Fatalf("decodeBIFFString() = %q, %d, %v", value, used, err)
	}
	value, used, err = decodeBIFFString(encodeWide("Åbo"), 3, true)
	if err != nil || value != "Åbo" || used != 6 {
		t.Fatalf("decodeBIFFString(wide) = %q, %d, %v", value, used, err)
	}
	for _, test := range []struct {
		data  []byte
		count int
		wide  bool
	}{{data: nil, count: -1}, {data: []byte("a"), count: 2}, {data: []byte{1}, count: 1, wide: true}} {
		if _, _, err = decodeBIFFString(test.data, test.count, test.wide); err == nil {
			t.Fatalf("decodeBIFFString(%v) returned nil error", test)
		}
	}
}

func TestDecodeRKHandlesIntegerFloatAndScaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  uint32
		want string
	}{
		{raw: uint32(42<<2) | 2, want: "42"},
		{raw: uint32(4250<<2) | 3, want: "42.5"},
		{raw: uint32(math.Float64bits(12.5) >> 32), want: "12.5"},
		{raw: uint32(math.Float64bits(1250)>>32) | 1, want: "12.5"},
	}
	for _, test := range tests {
		if got := decodeRK(test.raw); got != test.want {
			t.Fatalf("decodeRK(%x) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestExcelErrorReturnsKnownAndUnknownCodes(t *testing.T) {
	t.Parallel()

	for code, want := range map[byte]string{0x00: "#NULL!", 0x07: "#DIV/0!", 0x0f: "#VALUE!", 0x17: "#REF!", 0x1d: "#NAME?", 0x24: "#NUM!", 0x2a: "#N/A", 0xff: "#ERROR(255)"} {
		if got := excelError(code); got != want {
			t.Fatalf("excelError(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestParseSSTSupportsOptionsAndContinuation(t *testing.T) {
	t.Parallel()

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[:4], 3)
	binary.LittleEndian.PutUint32(header[4:], 3)
	first := append(append([]byte(nil), header...), 3, 0, 0, 'o', 'n', 'e')
	first = append(first, 2, 0, 1)
	first = append(first, encodeWide("Åb")...)
	first = append(first, 4, 0, 0, 'a', 'b')
	second := []byte{0, 'c', 'd'}
	strings, err := parseSST([][]byte{first, second})
	if err != nil || !reflect.DeepEqual(strings, []string{"one", "Åb", "abcd"}) {
		t.Fatalf("parseSST() = %#v, %v", strings, err)
	}

	richExtended := append([]byte{}, header...)
	binary.LittleEndian.PutUint32(richExtended[4:8], 1)
	richExtended = append(richExtended, 1, 0, 0x0c, 1, 0, 2, 0, 0, 0, 'x')
	richExtended = append(richExtended, make([]byte, 6)...)
	strings, err = parseSST([][]byte{richExtended})
	if err != nil || !reflect.DeepEqual(strings, []string{"x"}) {
		t.Fatalf("parseSST(rich) = %#v, %v", strings, err)
	}
}

func TestParseSSTRejectsMalformedData(t *testing.T) {
	t.Parallel()

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[4:], 1)
	tests := [][][]byte{
		{{1}},
		{append(append([]byte{}, header...), 2, 0)},
		{append(append([]byte{}, header...), 1, 0, 0x08)},
		{append(append([]byte{}, header...), 1, 0, 0x04)},
		{append(append([]byte{}, header...), 2, 0, 0, 'a')},
		{append(append([]byte{}, header...), 1, 0, 0x08, 1, 0, 'a')},
	}
	for _, segments := range tests {
		if _, err := parseSST(segments); err == nil {
			t.Fatalf("parseSST(%v) returned nil error", segments)
		}
	}

	invalidCount := make([]byte, 8)
	binary.LittleEndian.PutUint32(invalidCount[4:], 100)
	if _, err := parseSST([][]byte{invalidCount}); err == nil {
		t.Fatal("parseSST accepted impossible count")
	}
	if _, err := parseSST([][]byte{append(header, 2, 0, 0, 'a'), {}}); err == nil {
		t.Fatal("parseSST accepted empty continuation")
	}
}

func TestSegmentedReaderValidatesReads(t *testing.T) {
	t.Parallel()

	reader := &segmentedReader{segments: [][]byte{{1}, {2, 3}}}
	data, err := reader.readRaw(3)
	if err != nil || !reflect.DeepEqual(data, []byte{1, 2, 3}) || reader.remaining() != 0 {
		t.Fatalf("readRaw() = %v, %v, remaining %d", data, err, reader.remaining())
	}
	if _, err = reader.readRaw(1); err == nil {
		t.Fatal("readRaw accepted exhausted source")
	}
	if _, err = reader.readRaw(-1); err == nil {
		t.Fatal("readRaw accepted negative count")
	}
	reader = &segmentedReader{segments: [][]byte{{1}}}
	if _, err = reader.readRawWithinSegment(2); err == nil {
		t.Fatal("readRawWithinSegment crossed a boundary")
	}
}

func TestParseSheetDecodesSupportedCellRecords(t *testing.T) {
	t.Parallel()

	data := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	data = append(data, biffRecord(0x0208, rowPayload(0, 0, 6))...)
	data = append(data, biffRecord(0x00fd, labelSSTPayload(0, 0, 0))...)
	data = append(data, biffRecord(0x0203, numberPayload(0, 1, 12.5))...)
	data = append(data, biffRecord(0x027e, rkPayload(0, 2, uint32(42<<2)|2))...)
	data = append(data, biffRecord(0x00bd, mulRKPayload(0, 3, []uint32{uint32(1<<2) | 2, uint32(2<<2) | 2}))...)
	data = append(data, biffRecord(0x0205, boolErrPayload(0, 5, 1, false))...)
	data = append(data, biffRecord(0x0208, rowPayload(1, 0, 2))...)
	data = append(data, biffRecord(0x0205, boolErrPayload(1, 0, 0x07, true))...)
	data = append(data, biffRecord(0x000a, nil)...)

	rows, err := parseSheet(data, 0, []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Cell{
		{{Value: "name"}, {Value: "12.5"}, {Value: "42"}, {Value: "1"}, {Value: "2"}, {Value: "true"}},
		{{Error: "#DIV/0!"}, {}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("parseSheet() = %#v, want %#v", rows, want)
	}
}

func TestParseSheetRejectsMalformedRecords(t *testing.T) {
	t.Parallel()

	bof := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	tests := []struct {
		name   string
		record []byte
		shared []string
	}{
		{name: "row", record: biffRecord(0x0208, []byte{1})},
		{name: "label size", record: biffRecord(0x00fd, []byte{1})},
		{name: "label index", record: biffRecord(0x00fd, labelSSTPayload(0, 0, 2))},
		{name: "number", record: biffRecord(0x0203, []byte{1})},
		{name: "rk", record: biffRecord(0x027e, []byte{1})},
		{name: "mulrk size", record: biffRecord(0x00bd, []byte{1})},
		{name: "mulrk range", record: biffRecord(0x00bd, mulRKPayloadWithLast(0, 0, []uint32{2}, 2))},
		{name: "boolerr", record: biffRecord(0x0205, []byte{1})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append(append([]byte{}, bof...), test.record...)
			data = append(data, biffRecord(0x000a, nil)...)
			if _, err := parseSheet(data, 0, test.shared); err == nil {
				t.Fatal("parseSheet() returned nil error")
			}
		})
	}
	if _, err := parseSheet([]byte{1}, 0, nil); err == nil {
		t.Fatal("parseSheet accepted missing BOF")
	}
	truncated := append(append([]byte{}, bof...), 1, 0, 2)
	if _, err := parseSheet(truncated, 0, nil); err == nil {
		t.Fatal("parseSheet accepted truncated record")
	}
}

func TestParseBIFF8ValidatesGlobalsAndBuildsWorkbook(t *testing.T) {
	t.Parallel()

	data := syntheticWorkbook(t)
	workbook, err := parseBIFF8(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(workbook.Sheets) != 1 || workbook.Sheets[0].Name != "Data" || workbook.Sheets[0].Rows[0][0].Value != "value" {
		t.Fatalf("workbook = %#v", workbook)
	}
	for _, input := range [][]byte{
		nil,
		biffRecord(0x0809, []byte{0, 5, 0, 0}),
		append(biffRecord(0x0809, []byte{0, 6, 5, 0}), biffRecord(0x000a, nil)...),
	} {
		if _, err = parseBIFF8(input); err == nil {
			t.Fatalf("parseBIFF8(%v) returned nil error", input)
		}
	}

	badBound := append(biffRecord(0x0809, []byte{0, 6, 5, 0}), biffRecord(0x0085, []byte{1})...)
	if _, err = parseBIFF8(badBound); err == nil {
		t.Fatal("parseBIFF8 accepted bad BOUNDSHEET")
	}
	badSST := append(biffRecord(0x0809, []byte{0, 6, 5, 0}), biffRecord(0x00fc, []byte{1})...)
	if _, err = parseBIFF8(badSST); err == nil {
		t.Fatal("parseBIFF8 accepted bad SST")
	}
	truncatedGlobal := append(biffRecord(0x0809, []byte{0, 6, 5, 0}), []byte{1, 2, 3}...)
	if _, err = parseBIFF8(truncatedGlobal); err == nil {
		t.Fatal("parseBIFF8 accepted a truncated global record")
	}

	continuedSST := make([]byte, 8)
	binary.LittleEndian.PutUint32(continuedSST[:4], 1)
	binary.LittleEndian.PutUint32(continuedSST[4:], 1)
	continuedSST = append(continuedSST, 2, 0, 0, 'a')
	continued := append(biffRecord(0x0809, []byte{0, 6, 5, 0}), biffRecord(0x00fc, continuedSST)...)
	continued = append(continued, biffRecord(0x003c, []byte{0, 'b'})...)
	continued = append(continued, biffRecord(0x000a, nil)...)
	if _, err = parseBIFF8(continued); err == nil || !strings.Contains(err.Error(), "no sheets") {
		t.Fatalf("parseBIFF8 continued SST error = %v", err)
	}
	badOffset := append([]byte{}, data...)
	boundOffset := len(biffRecord(0x0809, []byte{0, 6, 5, 0})) + 4
	binary.LittleEndian.PutUint32(badOffset[boundOffset:boundOffset+4], uint32(len(data)+10))
	if _, err = parseBIFF8(badOffset); err == nil || !strings.Contains(err.Error(), "sheet") {
		t.Fatalf("parseBIFF8 bad offset error = %v", err)
	}
}

func TestReadCharactersRejectsSplitWideCharacter(t *testing.T) {
	t.Parallel()

	reader := &segmentedReader{segments: [][]byte{{1}, {0}}}
	if _, err := reader.readCharacters(1, true); err == nil {
		t.Fatal("readCharacters accepted a split wide character")
	}
}

func syntheticWorkbook(t *testing.T) []byte {
	t.Helper()
	bof := biffRecord(0x0809, []byte{0, 6, 5, 0})
	boundPayload := append([]byte{0, 0, 0, 0, 0, 0, 4, 0}, []byte("Data")...)
	bound := biffRecord(0x0085, boundPayload)
	sstPayload := make([]byte, 8)
	binary.LittleEndian.PutUint32(sstPayload[:4], 1)
	binary.LittleEndian.PutUint32(sstPayload[4:], 1)
	sstPayload = append(sstPayload, 5, 0, 0, 'v', 'a', 'l', 'u', 'e')
	sst := biffRecord(0x00fc, sstPayload)
	eof := biffRecord(0x000a, nil)
	globalsSize := len(bof) + len(bound) + len(sst) + len(eof)
	binary.LittleEndian.PutUint32(bound[4:8], uint32(globalsSize))
	sheet := biffRecord(0x0809, []byte{0, 6, 0x10, 0})
	sheet = append(sheet, biffRecord(0x00fd, labelSSTPayload(0, 0, 0))...)
	sheet = append(sheet, eof...)
	return append(append(append(append(bof, bound...), sst...), eof...), sheet...)
}

func biffRecord(id uint16, payload []byte) []byte {
	data := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint16(data[:2], id)
	binary.LittleEndian.PutUint16(data[2:4], uint16(len(payload)))
	copy(data[4:], payload)
	return data
}

func rowPayload(row, first, last int) []byte {
	data := make([]byte, 6)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(first))
	binary.LittleEndian.PutUint16(data[4:6], uint16(last))
	return data
}

func labelSSTPayload(row, column int, index uint32) []byte {
	data := make([]byte, 10)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(column))
	binary.LittleEndian.PutUint32(data[6:], index)
	return data
}

func numberPayload(row, column int, value float64) []byte {
	data := make([]byte, 14)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(column))
	binary.LittleEndian.PutUint64(data[6:], math.Float64bits(value))
	return data
}

func rkPayload(row, column int, raw uint32) []byte {
	data := make([]byte, 10)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(column))
	binary.LittleEndian.PutUint32(data[6:], raw)
	return data
}

func mulRKPayload(row, column int, values []uint32) []byte {
	return mulRKPayloadWithLast(row, column, values, column+len(values)-1)
}

func mulRKPayloadWithLast(row, column int, values []uint32, last int) []byte {
	data := make([]byte, 6+len(values)*6)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(column))
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[6+index*6:], value)
	}
	binary.LittleEndian.PutUint16(data[len(data)-2:], uint16(last))
	return data
}

func boolErrPayload(row, column int, value byte, isError bool) []byte {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint16(data[:2], uint16(row))
	binary.LittleEndian.PutUint16(data[2:4], uint16(column))
	data[6] = value
	if isError {
		data[7] = 1
	}
	return data
}

func encodeWide(value string) []byte {
	data := make([]byte, 0, len(value)*2)
	for _, character := range value {
		data = binary.LittleEndian.AppendUint16(data, uint16(character))
	}
	return data
}
