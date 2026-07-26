package tabular

import (
	"encoding/xml"
	"errors"
	"io"
	"path"
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

	decoder := xml.NewDecoder(reader)
	depth := 0
	sheetsDepth := 0
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return xlsxSheetReference{}, xlsxPresenceError(tokenErr)
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if sheetsDepth == 0 &&
				depth == 2 &&
				element.Name.Local == "sheets" {
				sheetsDepth = depth
				continue
			}
			if sheetsDepth == 0 ||
				depth != sheetsDepth+1 ||
				element.Name.Local != "sheet" {
				continue
			}
			reference := xlsxSheetReference{}
			for _, attribute := range element.Attr {
				switch attribute.Name.Local {
				case "name":
					reference.name = attribute.Value
				case "id":
					reference.relationshipID = attribute.Value
				}
			}
			if reference.name == "" || reference.relationshipID == "" {
				return xlsxSheetReference{}, xlsxPresenceError(errors.New(
					"worksheet declaration is invalid",
				))
			}
			if requested == "" || reference.name == requested {
				return reference, nil
			}
		case xml.EndElement:
			if depth == sheetsDepth {
				sheetsDepth = 0
			}
			depth--
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

	decoder := xml.NewDecoder(reader)
	depth := 0
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return "", xlsxPresenceError(tokenErr)
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth != 2 || element.Name.Local != "Relationship" {
				continue
			}
			var id, target, relationshipType, targetMode string
			for _, attribute := range element.Attr {
				switch attribute.Name.Local {
				case "Id":
					id = attribute.Value
				case "Target":
					target = attribute.Value
				case "Type":
					relationshipType = attribute.Value
				case "TargetMode":
					targetMode = attribute.Value
				}
			}
			if id != relationshipID {
				continue
			}
			if target == "" ||
				strings.EqualFold(targetMode, "External") ||
				!strings.HasSuffix(
					strings.ToLower(relationshipType),
					"/worksheet",
				) {
				return "", xlsxPresenceError(errors.New(
					"worksheet relationship is invalid",
				))
			}
			return target, nil
		case xml.EndElement:
			depth--
		}
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
			if element.Name.Local != "row" {
				continue
			}
			row := source.lastDeclaredRow + 1
			for _, attribute := range element.Attr {
				if attribute.Name.Local != "r" {
					continue
				}
				row, err = strconv.Atoi(attribute.Value)
				if err != nil || row <= 0 {
					return 0, nil, errors.New(
						"worksheet row reference is invalid",
					)
				}
			}
			if row <= source.lastDeclaredRow {
				return 0, nil, errors.New(
					"worksheet rows are not ordered",
				)
			}
			cells, err := source.readDeclaredRow(row)
			if err != nil {
				return 0, nil, err
			}
			source.lastDeclaredRow = row
			return row, cells, nil
		case xml.EndElement:
			if element.Name.Local == "sheetData" {
				return 0, nil, io.EOF
			}
		}
	}
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
				continue
			}
			column := nextColumn
			for _, attribute := range element.Attr {
				if attribute.Name.Local != "r" {
					continue
				}
				var referencedRow int
				column, referencedRow, err =
					excelize.CellNameToCoordinates(attribute.Value)
				if err != nil || referencedRow != row {
					return nil, errors.New(
						"worksheet cell reference is invalid",
					)
				}
			}
			if len(presence) < column {
				presence = append(
					presence,
					make([]bool, column-len(presence))...,
				)
			}
			presence[column-1] = true
			if column >= nextColumn {
				nextColumn = column + 1
			}
			if err = source.decoder.Skip(); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if element.Name.Local == "row" {
				return presence, nil
			}
		}
	}
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
