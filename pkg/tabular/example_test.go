package tabular_test

import (
	"fmt"
	"strings"

	tabular "github.com/faustbrian/golib/pkg/tabular"
)

func ExampleNewDelimitedReader() {
	reader, err := tabular.NewDelimitedReader(strings.NewReader("name;city\nAda;Helsinki\n"), tabular.DelimitedConfig{
		Delimiter: ';',
		Header: &tabular.HeaderConfig{
			Case:             tabular.HeaderCaseLower,
			RejectEmpty:      true,
			RejectDuplicates: true,
		},
	})
	if err != nil {
		panic(err)
	}
	header, err := reader.Header()
	if err != nil {
		panic(err)
	}
	row, err := reader.Read()
	if err != nil {
		panic(err)
	}
	fmt.Println(header)
	fmt.Println(row)
	// Output:
	// [name city]
	// [Ada Helsinki]
}

func ExampleNewFixedWidthReader() {
	reader, err := tabular.NewFixedWidthReader(strings.NewReader("001Ada       Helsinki  \n"), tabular.FixedWidthConfig{
		Fields: []tabular.FixedWidthField{
			{Name: "id", Start: 0, End: 3},
			{Name: "name", Start: 3, End: 13, TrimSpace: true},
			{Name: "city", Start: 13, End: 23, TrimSpace: true},
		},
	})
	if err != nil {
		panic(err)
	}
	row, err := reader.Read()
	if err != nil {
		panic(err)
	}
	fmt.Println(reader.Fields())
	fmt.Println(row)
	// Output:
	// [id name city]
	// [001 Ada Helsinki]
}
