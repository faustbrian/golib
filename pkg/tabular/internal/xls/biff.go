package xls

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
)

// Cell is a decoded BIFF cell value. Error is non-empty for Excel error cells.
type Cell struct {
	Value string
	Error string
}

// Sheet is one materialized BIFF worksheet.
type Sheet struct {
	Name string
	Rows [][]Cell
}

// Workbook is a bounded, materialized BIFF8 workbook.
type Workbook struct {
	Sheets []Sheet
}

type boundSheet struct {
	name   string
	offset uint32
}

type record struct {
	id      uint16
	payload []byte
	next    int
}

// Open parses the Workbook stream from an OLE2 compound file.
func Open(data []byte) (*Workbook, error) {
	stream, err := openWorkbookStream(data)
	if err != nil {
		return nil, err
	}
	return parseBIFF8(stream)
}

func parseBIFF8(data []byte) (*Workbook, error) {
	first, err := readRecord(data, 0)
	if err != nil || first.id != 0x0809 || len(first.payload) < 4 || binary.LittleEndian.Uint16(first.payload[:2]) != 0x0600 {
		return nil, errors.New("xls: BIFF8 workbook globals not found")
	}
	var sheets []boundSheet
	var shared []string
	for offset := 0; offset < len(data); {
		rec, recErr := readRecord(data, offset)
		if recErr != nil {
			return nil, recErr
		}
		switch rec.id {
		case 0x0085:
			sheet, sheetErr := parseBoundSheet(rec.payload)
			if sheetErr != nil {
				return nil, sheetErr
			}
			sheets = append(sheets, sheet)
		case 0x00fc:
			segments := [][]byte{rec.payload}
			next := rec.next
			for next < len(data) {
				continuation, continuationErr := readRecord(data, next)
				if continuationErr != nil || continuation.id != 0x003c {
					break
				}
				segments = append(segments, continuation.payload)
				next = continuation.next
			}
			shared, recErr = parseSST(segments)
			if recErr != nil {
				return nil, recErr
			}
			rec.next = next
		case 0x000a:
			offset = len(data)
			continue
		}
		offset = rec.next
	}
	if len(sheets) == 0 {
		return nil, errors.New("xls: workbook contains no sheets")
	}

	workbook := &Workbook{Sheets: make([]Sheet, 0, len(sheets))}
	for _, definition := range sheets {
		rows, rowErr := parseSheet(data, int(definition.offset), shared)
		if rowErr != nil {
			return nil, fmt.Errorf("xls: sheet %q: %w", definition.name, rowErr)
		}
		workbook.Sheets = append(workbook.Sheets, Sheet{Name: definition.name, Rows: rows})
	}
	return workbook, nil
}

func readRecord(data []byte, offset int) (record, error) {
	if offset < 0 || offset+4 > len(data) {
		return record{}, errors.New("xls: truncated BIFF record header")
	}
	size := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
	next := offset + 4 + size
	if next > len(data) {
		return record{}, errors.New("xls: truncated BIFF record payload")
	}
	return record{id: binary.LittleEndian.Uint16(data[offset : offset+2]), payload: data[offset+4 : next], next: next}, nil
}

func parseBoundSheet(data []byte) (boundSheet, error) {
	if len(data) < 8 {
		return boundSheet{}, errors.New("xls: truncated BOUNDSHEET record")
	}
	count := int(data[6])
	wide := data[7]&1 != 0
	name, used, err := decodeBIFFString(data[8:], count, wide)
	if err != nil || used != len(data)-8 {
		return boundSheet{}, errors.New("xls: invalid sheet name")
	}
	return boundSheet{name: name, offset: binary.LittleEndian.Uint32(data[:4])}, nil
}

func parseSheet(data []byte, offset int, shared []string) ([][]Cell, error) {
	first, err := readRecord(data, offset)
	if err != nil || first.id != 0x0809 {
		return nil, errors.New("worksheet BOF not found")
	}
	rows := make(map[int]map[int]Cell)
	widths := make(map[int]int)
	maxRow := -1
	for offset < len(data) {
		rec, recErr := readRecord(data, offset)
		if recErr != nil {
			return nil, recErr
		}
		if rec.id == 0x000a {
			break
		}
		switch rec.id {
		case 0x0208:
			if len(rec.payload) < 6 {
				return nil, errors.New("truncated ROW record")
			}
			row := int(binary.LittleEndian.Uint16(rec.payload[:2]))
			last := int(binary.LittleEndian.Uint16(rec.payload[4:6]))
			widths[row] = max(widths[row], last)
			maxRow = max(maxRow, row)
		case 0x00fd:
			if len(rec.payload) != 10 {
				return nil, errors.New("invalid LABELSST record")
			}
			row, column := cellPosition(rec.payload)
			index := binary.LittleEndian.Uint32(rec.payload[6:10])
			if uint64(index) >= uint64(len(shared)) {
				return nil, errors.New("shared string index outside SST")
			}
			setCell(rows, widths, row, column, Cell{Value: shared[index]})
			maxRow = max(maxRow, row)
		case 0x0203:
			if len(rec.payload) != 14 {
				return nil, errors.New("invalid NUMBER record")
			}
			row, column := cellPosition(rec.payload)
			value := math.Float64frombits(binary.LittleEndian.Uint64(rec.payload[6:14]))
			setCell(rows, widths, row, column, Cell{Value: strconv.FormatFloat(value, 'f', -1, 64)})
			maxRow = max(maxRow, row)
		case 0x027e:
			if len(rec.payload) != 10 {
				return nil, errors.New("invalid RK record")
			}
			row, column := cellPosition(rec.payload)
			setCell(rows, widths, row, column, Cell{Value: decodeRK(binary.LittleEndian.Uint32(rec.payload[6:10]))})
			maxRow = max(maxRow, row)
		case 0x00bd:
			if len(rec.payload) < 12 || (len(rec.payload)-6)%6 != 0 {
				return nil, errors.New("invalid MULRK record")
			}
			row := int(binary.LittleEndian.Uint16(rec.payload[:2]))
			column := int(binary.LittleEndian.Uint16(rec.payload[2:4]))
			count := (len(rec.payload) - 6) / 6
			last := int(binary.LittleEndian.Uint16(rec.payload[len(rec.payload)-2:]))
			if last != column+count-1 {
				return nil, errors.New("inconsistent MULRK range")
			}
			for index := 0; index < count; index++ {
				start := 4 + index*6
				setCell(rows, widths, row, column+index, Cell{Value: decodeRK(binary.LittleEndian.Uint32(rec.payload[start+2 : start+6]))})
			}
			maxRow = max(maxRow, row)
		case 0x0205:
			if len(rec.payload) != 8 {
				return nil, errors.New("invalid BOOLERR record")
			}
			row, column := cellPosition(rec.payload)
			cell := Cell{}
			if rec.payload[7] != 0 {
				cell.Error = excelError(rec.payload[6])
			} else {
				cell.Value = strconv.FormatBool(rec.payload[6] != 0)
			}
			setCell(rows, widths, row, column, cell)
			maxRow = max(maxRow, row)
		}
		offset = rec.next
	}
	result := make([][]Cell, maxRow+1)
	for rowIndex := range result {
		result[rowIndex] = make([]Cell, widths[rowIndex])
		for column, cell := range rows[rowIndex] {
			result[rowIndex][column] = cell
		}
	}
	return result, nil
}

func cellPosition(payload []byte) (int, int) {
	return int(binary.LittleEndian.Uint16(payload[:2])), int(binary.LittleEndian.Uint16(payload[2:4]))
}

func setCell(rows map[int]map[int]Cell, widths map[int]int, row, column int, cell Cell) {
	if rows[row] == nil {
		rows[row] = make(map[int]Cell)
	}
	rows[row][column] = cell
	widths[row] = max(widths[row], column+1)
}

func decodeRK(raw uint32) string {
	scaled := raw&1 != 0
	if raw&2 != 0 {
		value := float64(int32(raw) >> 2)
		if scaled {
			value /= 100
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	value := math.Float64frombits(uint64(raw&^3) << 32)
	if scaled {
		value /= 100
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func excelError(code byte) string {
	values := map[byte]string{0x00: "#NULL!", 0x07: "#DIV/0!", 0x0f: "#VALUE!", 0x17: "#REF!", 0x1d: "#NAME?", 0x24: "#NUM!", 0x2a: "#N/A"}
	if value, ok := values[code]; ok {
		return value
	}
	return fmt.Sprintf("#ERROR(%d)", code)
}

func decodeBIFFString(data []byte, count int, wide bool) (string, int, error) {
	width := 1
	if wide {
		width = 2
	}
	if count < 0 || count > len(data)/width {
		return "", 0, errors.New("xls: truncated string")
	}
	if !wide {
		runes := make([]rune, count)
		for index := range runes {
			runes[index] = rune(data[index])
		}
		return string(runes), count, nil
	}
	units := make([]uint16, count)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2 : index*2+2])
	}
	return string(utf16.Decode(units)), count * 2, nil
}

type segmentedReader struct {
	segments [][]byte
	segment  int
	offset   int
}

func parseSST(segments [][]byte) ([]string, error) {
	reader := &segmentedReader{segments: segments}
	header, err := reader.readRaw(8)
	if err != nil {
		return nil, errors.New("xls: truncated SST header")
	}
	count := binary.LittleEndian.Uint32(header[4:8])
	if uint64(count) > uint64(reader.remaining())/3+1 {
		return nil, errors.New("xls: invalid SST string count")
	}
	strings := make([]string, 0, count)
	for index := uint32(0); index < count; index++ {
		prefix, prefixErr := reader.readRaw(3)
		if prefixErr != nil {
			return nil, errors.New("xls: truncated SST string")
		}
		characters := int(binary.LittleEndian.Uint16(prefix[:2]))
		flags := prefix[2]
		var richRuns uint16
		var extension uint32
		if flags&0x08 != 0 {
			data, readErr := reader.readRaw(2)
			if readErr != nil {
				return nil, readErr
			}
			richRuns = binary.LittleEndian.Uint16(data)
		}
		if flags&0x04 != 0 {
			data, readErr := reader.readRaw(4)
			if readErr != nil {
				return nil, readErr
			}
			extension = binary.LittleEndian.Uint32(data)
		}
		value, readErr := reader.readCharacters(characters, flags&1 != 0)
		if readErr != nil {
			return nil, readErr
		}
		if _, readErr = reader.readRaw(int(richRuns)*4 + int(extension)); readErr != nil {
			return nil, readErr
		}
		strings = append(strings, value)
	}
	return strings, nil
}

func (reader *segmentedReader) readCharacters(count int, wide bool) (string, error) {
	units := make([]uint16, count)
	for index := 0; index < count; index++ {
		if reader.atBoundary() {
			reader.segment++
			reader.offset = 0
			option, err := reader.readRaw(1)
			if err != nil {
				return "", errors.New("xls: truncated SST continuation")
			}
			wide = option[0]&1 != 0
		}
		width := 1
		if wide {
			width = 2
		}
		data, err := reader.readRawWithinSegment(width)
		if err != nil {
			return "", errors.New("xls: split SST character")
		}
		if wide {
			units[index] = binary.LittleEndian.Uint16(data)
		} else {
			units[index] = uint16(data[0])
		}
	}
	return string(utf16.Decode(units)), nil
}

func (reader *segmentedReader) readRaw(count int) ([]byte, error) {
	if count < 0 || count > reader.remaining() {
		return nil, errors.New("xls: truncated segmented data")
	}
	result := make([]byte, 0, count)
	for len(result) < count {
		if reader.atBoundary() {
			reader.segment++
			reader.offset = 0
		}
		available := len(reader.segments[reader.segment]) - reader.offset
		take := min(count-len(result), available)
		result = append(result, reader.segments[reader.segment][reader.offset:reader.offset+take]...)
		reader.offset += take
	}
	return result, nil
}

func (reader *segmentedReader) readRawWithinSegment(count int) ([]byte, error) {
	if reader.segment >= len(reader.segments) || reader.offset+count > len(reader.segments[reader.segment]) {
		return nil, errors.New("xls: segment boundary")
	}
	data := reader.segments[reader.segment][reader.offset : reader.offset+count]
	reader.offset += count
	return data, nil
}

func (reader *segmentedReader) atBoundary() bool {
	return reader.segment < len(reader.segments) && reader.offset == len(reader.segments[reader.segment])
}

func (reader *segmentedReader) remaining() int {
	remaining := 0
	for index, segment := range reader.segments {
		if index < reader.segment {
			continue
		}
		if index == reader.segment {
			remaining += len(segment) - reader.offset
		} else {
			remaining += len(segment)
		}
	}
	return remaining
}
