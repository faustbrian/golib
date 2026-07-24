// Package xls implements the bounded OLE2 primitives needed to read BIFF8
// workbooks. It is intentionally internal and is not a general OLE library.
package xls

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf16"
)

const (
	endOfChain = uint32(0xfffffffe)
	freeSector = uint32(0xffffffff)
)

var compoundSignature = [8]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

type compoundFile struct {
	data           []byte
	sectorSize     int
	miniSectorSize int
	miniCutoff     uint64
	fat            []uint32
	miniFat        []uint32
	miniStream     []byte
	directories    []directory
}

type directory struct {
	name   string
	typeID byte
	start  uint32
	size   uint64
}

func openWorkbookStream(data []byte) ([]byte, error) {
	compound, err := parseCompoundFile(data)
	if err != nil {
		return nil, err
	}
	for _, entry := range compound.directories {
		if entry.typeID == 2 && (entry.name == "Workbook" || entry.name == "Book") {
			return compound.readEntry(entry)
		}
	}
	return nil, errors.New("xls: workbook stream not found")
}

func parseCompoundFile(data []byte) (*compoundFile, error) {
	if len(data) < 512 || string(data[:8]) != string(compoundSignature[:]) {
		return nil, errors.New("xls: invalid OLE2 signature")
	}
	if binary.LittleEndian.Uint16(data[28:30]) != 0xfffe {
		return nil, errors.New("xls: unsupported OLE2 byte order")
	}
	major := binary.LittleEndian.Uint16(data[26:28])
	sectorShift := binary.LittleEndian.Uint16(data[30:32])
	miniShift := binary.LittleEndian.Uint16(data[32:34])
	if (major != 3 && major != 4) || (sectorShift != 9 && sectorShift != 12) || miniShift != 6 {
		return nil, errors.New("xls: unsupported OLE2 version")
	}
	sectorSize := 1 << sectorShift
	if len(data) < sectorSize || len(data)%sectorSize != 0 {
		return nil, errors.New("xls: truncated OLE2 sectors")
	}
	compound := &compoundFile{
		data:           data,
		sectorSize:     sectorSize,
		miniSectorSize: 1 << miniShift,
		miniCutoff:     uint64(binary.LittleEndian.Uint32(data[56:60])),
	}

	numFAT := binary.LittleEndian.Uint32(data[44:48])
	if numFAT > uint32(compound.sectorCount()) {
		return nil, errors.New("xls: invalid FAT sector count")
	}
	difat := make([]uint32, 0, numFAT)
	for offset := 76; offset < 512 && uint32(len(difat)) < numFAT; offset += 4 {
		sector := binary.LittleEndian.Uint32(data[offset : offset+4])
		if sector != freeSector {
			difat = append(difat, sector)
		}
	}
	difatStart := binary.LittleEndian.Uint32(data[68:72])
	numDIFAT := binary.LittleEndian.Uint32(data[72:76])
	seenDIFAT := make(map[uint32]struct{}, numDIFAT)
	for index, sectorID := uint32(0), difatStart; index < numDIFAT; index++ {
		if sectorID == endOfChain || sectorID == freeSector {
			return nil, errors.New("xls: truncated DIFAT chain")
		}
		if _, exists := seenDIFAT[sectorID]; exists {
			return nil, errors.New("xls: cyclic DIFAT chain")
		}
		seenDIFAT[sectorID] = struct{}{}
		sector, err := compound.sector(sectorID)
		if err != nil {
			return nil, err
		}
		for offset := 0; offset < sectorSize-4 && uint32(len(difat)) < numFAT; offset += 4 {
			fatID := binary.LittleEndian.Uint32(sector[offset : offset+4])
			if fatID != freeSector {
				difat = append(difat, fatID)
			}
		}
		sectorID = binary.LittleEndian.Uint32(sector[sectorSize-4:])
	}
	if uint32(len(difat)) != numFAT {
		return nil, errors.New("xls: incomplete FAT index")
	}
	for _, sectorID := range difat {
		sector, err := compound.sector(sectorID)
		if err != nil {
			return nil, err
		}
		for offset := 0; offset < sectorSize; offset += 4 {
			compound.fat = append(compound.fat, binary.LittleEndian.Uint32(sector[offset:offset+4]))
		}
	}

	directoryBytes, err := compound.readRegularStream(binary.LittleEndian.Uint32(data[48:52]), 0)
	if err != nil {
		return nil, fmt.Errorf("xls: read directory: %w", err)
	}
	for offset := 0; offset+128 <= len(directoryBytes); offset += 128 {
		entry := directoryBytes[offset : offset+128]
		typeID := entry[66]
		if typeID == 0 {
			continue
		}
		nameBytes := int(binary.LittleEndian.Uint16(entry[64:66]))
		if nameBytes < 2 || nameBytes > 64 || nameBytes%2 != 0 {
			return nil, errors.New("xls: invalid directory name")
		}
		nameUnits := make([]uint16, nameBytes/2-1)
		for index := range nameUnits {
			nameUnits[index] = binary.LittleEndian.Uint16(entry[index*2 : index*2+2])
		}
		size := binary.LittleEndian.Uint64(entry[120:128])
		if major == 3 {
			size &= math.MaxUint32
		}
		compound.directories = append(compound.directories, directory{
			name:   string(utf16.Decode(nameUnits)),
			typeID: typeID,
			start:  binary.LittleEndian.Uint32(entry[116:120]),
			size:   size,
		})
	}

	var root *directory
	for index := range compound.directories {
		if compound.directories[index].typeID == 5 {
			root = &compound.directories[index]
			break
		}
	}
	if root == nil {
		return nil, errors.New("xls: root directory not found")
	}
	miniFATStart := binary.LittleEndian.Uint32(data[60:64])
	numMiniFAT := binary.LittleEndian.Uint32(data[64:68])
	if numMiniFAT > 0 {
		miniBytes, err := compound.readRegularStream(miniFATStart, uint64(numMiniFAT)*uint64(sectorSize))
		if err != nil {
			return nil, fmt.Errorf("xls: read mini FAT: %w", err)
		}
		for offset := 0; offset+4 <= len(miniBytes); offset += 4 {
			compound.miniFat = append(compound.miniFat, binary.LittleEndian.Uint32(miniBytes[offset:offset+4]))
		}
	}
	if root.size > 0 {
		compound.miniStream, err = compound.readRegularStream(root.start, root.size)
		if err != nil {
			return nil, fmt.Errorf("xls: read mini stream: %w", err)
		}
	}
	return compound, nil
}

func (compound *compoundFile) readEntry(entry directory) ([]byte, error) {
	if entry.size < compound.miniCutoff {
		return compound.readMiniStream(entry.start, entry.size)
	}
	return compound.readRegularStream(entry.start, entry.size)
}

func (compound *compoundFile) readRegularStream(start uint32, size uint64) ([]byte, error) {
	chain, err := compound.chain(start, compound.fat, compound.sectorCount())
	if err != nil {
		return nil, err
	}
	if size == 0 {
		size = uint64(len(chain) * compound.sectorSize)
	}
	if size > uint64(len(chain))*uint64(compound.sectorSize) || size > uint64(len(compound.data)) {
		return nil, errors.New("xls: stream size exceeds sector chain")
	}
	stream := make([]byte, 0, len(chain)*compound.sectorSize)
	for _, sectorID := range chain {
		sector, _ := compound.sector(sectorID)
		stream = append(stream, sector...)
	}
	return stream[:int(size)], nil
}

func (compound *compoundFile) readMiniStream(start uint32, size uint64) ([]byte, error) {
	chain, err := compound.chain(start, compound.miniFat, len(compound.miniFat))
	if err != nil {
		return nil, err
	}
	if size > uint64(len(chain))*uint64(compound.miniSectorSize) || size > uint64(len(compound.miniStream)) {
		return nil, errors.New("xls: mini stream size exceeds chain")
	}
	stream := make([]byte, 0, len(chain)*compound.miniSectorSize)
	for _, sectorID := range chain {
		offset := uint64(sectorID) * uint64(compound.miniSectorSize)
		if offset+uint64(compound.miniSectorSize) > uint64(len(compound.miniStream)) {
			return nil, errors.New("xls: mini sector outside root stream")
		}
		stream = append(stream, compound.miniStream[offset:offset+uint64(compound.miniSectorSize)]...)
	}
	return stream[:int(size)], nil
}

func (compound *compoundFile) chain(start uint32, table []uint32, limit int) ([]uint32, error) {
	if start == endOfChain && limit == 0 {
		return nil, nil
	}
	chain := make([]uint32, 0)
	seen := make(map[uint32]struct{})
	for current := start; current != endOfChain; current = table[current] {
		if current == freeSector || int(current) >= len(table) || len(chain) >= limit {
			return nil, errors.New("xls: invalid sector chain")
		}
		if _, exists := seen[current]; exists {
			return nil, errors.New("xls: cyclic sector chain")
		}
		seen[current] = struct{}{}
		chain = append(chain, current)
	}
	return chain, nil
}

func (compound *compoundFile) sectorCount() int {
	return len(compound.data)/compound.sectorSize - 1
}

func (compound *compoundFile) sector(id uint32) ([]byte, error) {
	if int(id) >= compound.sectorCount() {
		return nil, errors.New("xls: sector outside file")
	}
	offset := (uint64(id) + 1) * uint64(compound.sectorSize)
	return compound.data[offset : offset+uint64(compound.sectorSize)], nil
}
