package postgres_test

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
)

func ExampleDate() {
	value := calendarpg.NewDate(calendar.MustDate(2024, time.February, 29))
	encoded, _ := value.Value()
	fmt.Println(encoded)
	// Output:
	// 2024-02-29
}

func ExampleInfinityDate() {
	value := calendarpg.NewInfinityDate(calendarpg.PositiveInfinity)
	encoded, _ := value.Value()
	fmt.Println(encoded)
	// Output:
	// infinity
}
