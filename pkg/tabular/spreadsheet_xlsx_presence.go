package tabular

import (
	"encoding/xml"
	"errors"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type xlsxPresenceReader interface {
	Read() ([]bool, error)
	Close() error
}

type xlsxPresenceSource struct {
	reader          io.ReadCloser
	decoder         *xml.Decoder
	currentRow      int
	lastDeclaredRow int
	pendingRow      int
	pendingCells    []bool
}

type xlsxSheetReference struct {
	name           string
	relationshipID string
}

func openXLSXPresence(
	archive *ZIPArchive,
	sheet string,
) (*xlsxPresenceSource, error) {
	entry, err := selectedXLSXWorksheetEntry(archive, sheet)
	if err != nil {
		return nil, err
	}
	reader, err := archive.Open(entry)
	if err != nil {
		return nil, err
	}
	return &xlsxPresenceSource{
		reader:  reader,
		decoder: xml.NewDecoder(reader),
	}, nil
}

func selectedXLSXWorksheetEntry(
	archive *ZIPArchive,
	sheet string,
) (string, error) {
	reference, err := selectedXLSXSheetReference(archive, sheet)
	if err != nil {
		return "", err
	}
	target, err := xlsxWorksheetTarget(
		archive,
		reference.relationshipID,
	)
	if err != nil {
		return "", err
	}
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = path.Join("xl", target)
	}
	target = path.Clean(target)
	if !safeZIPName(target) {
		return "", xlsxPresenceError(errors.New(
			"worksheet relationship target is invalid",
		))
	}
	return target, nil
}

func selectedXLSXSheetReference(
	archive *ZIPArchive,
	requested string,
) (xlsxSheetReference, error) {
	reader, err := archive.Open("xl/workbook.xml")
	if err != nil {
		return xlsxSheetReference{}, err
	}
	defer func() { _ = reader.Close() }()

	var workbook struct {
		Sheets []struct {
			Name           string `xml:"name,attr"`
			RelationshipID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = xml.NewDecoder(reader).Decode(&workbook); err != nil {
		return xlsxSheetReference{}, xlsxPresenceError(err)
	}
	for _, sheet := range workbook.Sheets {
		if sheet.Name == "" || sheet.RelationshipID == "" {
			return xlsxSheetReference{}, xlsxPresenceError(errors.New("worksheet declaration is invalid"))
		}
		if requested == "" || sheet.Name == requested {
			return xlsxSheetReference{name: sheet.Name, relationshipID: sheet.RelationshipID}, nil
		}
	}
	return xlsxSheetReference{}, xlsxPresenceError(errors.New(
		"worksheet declaration was not found",
	))
}

func xlsxWorksheetTarget(
	archive *ZIPArchive,
	relationshipID string,
) (string, error) {
	reader, err := archive.Open("xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	var relationships struct {
		Entries []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			Type       string `xml:"Type,attr"`
			TargetMode string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err = xml.NewDecoder(reader).Decode(&relationships); err != nil {
		return "", xlsxPresenceError(err)
	}
	for _, relationship := range relationships.Entries {
		if relationship.ID != relationshipID {
			continue
		}
		if relationship.Target == "" ||
			strings.EqualFold(relationship.TargetMode, "External") ||
			!strings.HasSuffix(strings.ToLower(relationship.Type), "/worksheet") {
			return "", xlsxPresenceError(errors.New("worksheet relationship is invalid"))
		}
		return relationship.Target, nil
	}
	return "", xlsxPresenceError(errors.New(
		"worksheet relationship was not found",
	))
}

func (source *xlsxPresenceSource) Read() ([]bool, error) {
	requested := source.currentRow + 1
	if source.pendingRow != 0 {
		if source.pendingRow > requested {
			source.currentRow = requested
			return nil, nil
		}
		if source.pendingRow != requested {
			return nil, errors.New("worksheet rows are not ordered")
		}
		source.currentRow = requested
		cells := source.pendingCells
		source.pendingRow = 0
		source.pendingCells = nil
		return cells, nil
	}

	row, cells, err := source.nextDeclaredRow()
	if err != nil {
		return nil, err
	}
	if row > requested {
		source.pendingRow = row
		source.pendingCells = cells
		source.currentRow = requested
		return nil, nil
	}
	if row != requested {
		return nil, errors.New("worksheet rows are not ordered")
	}
	source.currentRow = requested
	return cells, nil
}

func (source *xlsxPresenceSource) nextDeclaredRow() (
	int,
	[]bool,
	error,
) {
	for {
		token, err := source.decoder.Token()
		if err != nil {
			return 0, nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "row" {
				return source.readDeclaredRowElement(element)
			}
		}
	}
}

func (source *xlsxPresenceSource) readDeclaredRowElement(element xml.StartElement) (int, []bool, error) {
	row := source.lastDeclaredRow + 1
	if reference, ok := xlsxAttribute(element.Attr, "r"); ok {
		parsed, err := strconv.Atoi(reference)
		if err != nil {
			return 0, nil, errors.New("worksheet row reference is invalid")
		}
		row = parsed
		if row <= 0 {
			return 0, nil, errors.New("worksheet row reference is invalid")
		}
	}
	if row <= source.lastDeclaredRow {
		return 0, nil, errors.New("worksheet rows are not ordered")
	}
	cells, err := source.readDeclaredRow(row)
	if err != nil {
		return 0, nil, err
	}
	source.lastDeclaredRow = row
	return row, cells, nil
}

func (source *xlsxPresenceSource) readDeclaredRow(row int) ([]bool, error) {
	var presence []bool
	nextColumn := 1
	for {
		token, err := source.decoder.Token()
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "c" {
				if err = source.decoder.Skip(); err != nil {
					return nil, err
				}
			} else {
				column := nextColumn
				if reference, ok := xlsxAttribute(element.Attr, "r"); ok {
					var referencedRow int
					column, referencedRow, err = excelize.CellNameToCoordinates(reference)
					if err != nil || referencedRow != row {
						return nil, errors.New("worksheet cell reference is invalid")
					}
				}
				presence = append(presence, make([]bool, max(0, column-len(presence)))...)
				presence[column-1] = true
				nextColumn = max(nextColumn, column+1)
				if err = source.decoder.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if element.Name.Local == "row" {
				return presence, nil
			}
		}
	}
}

func xlsxAttribute(attributes []xml.Attr, localName string) (string, bool) {
	index := slices.IndexFunc(attributes, func(attribute xml.Attr) bool {
		return attribute.Name.Local == localName
	})
	if index < 0 {
		return "", false
	}
	return attributes[index].Value, true
}

func (source *xlsxPresenceSource) Close() error {
	return source.reader.Close()
}

func xlsxPresenceError(err error) *Error {
	return &Error{
		Kind:   ErrorSpreadsheet,
		Op:     "spreadsheet.presence",
		Format: string(FormatXLSX),
		Err:    err,
	}
}
