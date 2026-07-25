package postgres_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func BenchmarkPGXBinaryDateEncode(b *testing.B) {
	codec := pgtype.NewMap()
	value := calendarpg.NewDate(calendar.MustDate(2024, time.February, 29))
	for b.Loop() {
		_, _ = codec.Encode(pgtype.DateOID, pgtype.BinaryFormatCode, value, nil)
	}
}
