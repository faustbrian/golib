package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
	"unicode/utf16"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

type request struct {
	Source    string `json:"source"`
	Flags     string `json:"flags"`
	Input     string `json:"input"`
	LastIndex int    `json:"lastIndex"`
}

type capture struct {
	Value *string `json:"value"`
	Start int     `json:"start"`
	End   int     `json:"end"`
}

type response struct {
	Matched   bool               `json:"matched"`
	LastIndex int                `json:"lastIndex"`
	Captures  []capture          `json:"captures,omitempty"`
	Names     map[string]capture `json:"names,omitempty"`
	NameOrder []string           `json:"nameOrder,omitempty"`
	Error     string             `json:"error,omitempty"`
}

func main() {
	var request request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		write(response{Error: err.Error()})
		return
	}

	source, err := decodePattern(request.Source)
	if err != nil {
		write(response{Error: err.Error()})
		return
	}
	input, err := decodeUTF16(request.Input)
	if err != nil {
		write(response{Error: err.Error()})
		return
	}
	program, err := ecmascript.Compile(
		source,
		request.Flags,
		ecmascript.DefaultCompileOptions(),
	)
	if err != nil {
		write(response{Error: err.Error()})
		return
	}

	limits := ecmascript.DefaultMatchOptions().Limits
	limits.InputBytes = max(uint64(len(input.Units())*2), 1)
	limits.InputRunes = max(uint64(len(input.Units())), 1)
	limits.Steps = max(uint64(len(input.Units())*2_000), 200_000_000)
	limits.Backtracks = max(uint64(len(input.Units())*1_000), 100_000_000)
	limits.StackDepth = 1_000_000
	limits.Allocations = max(uint64(len(input.Units())*2_000), 200_000_000)
	limits.WallTime = 30 * time.Second

	session := ecmascript.NewSession(program)
	session.SetLastIndex(request.LastIndex)
	result, matched, err := session.ExecUTF16(context.Background(), input, limits)
	if err != nil {
		write(response{LastIndex: session.LastIndex(), Error: err.Error()})
		return
	}
	answer := response{Matched: matched, LastIndex: session.LastIndex()}
	if matched {
		answer.Captures = make([]capture, 0, program.CaptureCount()+1)
		for _, item := range result.Captures() {
			answer.Captures = append(answer.Captures, encodeCapture(item))
		}
		nameIndices := program.CaptureNameIndices()
		answer.Names = make(map[string]capture, len(nameIndices))
		answer.NameOrder = make([]string, 0, len(nameIndices))
		for name := range nameIndices {
			item, _ := result.Named(name)
			answer.Names[name] = encodeCapture(item)
			answer.NameOrder = append(answer.NameOrder, name)
		}
		sort.Slice(answer.NameOrder, func(left, right int) bool {
			return nameIndices[answer.NameOrder[left]][0] <
				nameIndices[answer.NameOrder[right]][0]
		})
	}
	write(answer)
}

func decodePattern(encoded string) (string, error) {
	value, err := decodeUTF16(encoded)
	if err != nil {
		return "", err
	}
	units := value.Units()
	runes := make([]rune, 0, len(units))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		if utf16.IsSurrogate(rune(unit)) {
			if index+1 < len(units) && unit >= 0xD800 && unit <= 0xDBFF &&
				units[index+1] >= 0xDC00 && units[index+1] <= 0xDFFF {
				runes = append(runes, utf16.DecodeRune(rune(unit), rune(units[index+1])))
				index++
				continue
			}
			runes = append(runes, []rune(fmt.Sprintf(`\u%04X`, unit))...)
			continue
		}
		runes = append(runes, rune(unit))
	}
	return string(runes), nil
}

func decodeUTF16(encoded string) (ecmascript.UTF16String, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ecmascript.UTF16String{}, err
	}
	if len(data)%2 != 0 {
		return ecmascript.UTF16String{}, fmt.Errorf("odd UTF-16 byte count")
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	return ecmascript.UTF16FromUnits(units), nil
}

func encodeCapture(item ecmascript.Capture) capture {
	if !item.Participated() {
		return capture{Start: -1, End: -1}
	}
	units := item.Value().Units()
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	value := base64.StdEncoding.EncodeToString(data)
	span := item.Span()
	return capture{Value: &value, Start: span.Start.UTF16, End: span.End.UTF16}
}

func write(answer response) {
	if err := json.NewEncoder(os.Stdout).Encode(answer); err != nil {
		panic(err)
	}
}
