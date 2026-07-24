package international_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	intlLanguage "github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
	textlanguage "golang.org/x/text/language"
)

func TestConcurrentParsingFormattingAndMetadataSnapshots(t *testing.T) {
	t.Parallel()

	const workers = 8
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				if err := exerciseConcurrentInternationalOperations(); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func exerciseConcurrentInternationalOperations() error {
	finland, err := country.Parse("FI")
	if err != nil || country.Name(finland, textlanguage.Finnish) != "Suomi" ||
		len(country.DatasetRecords()) != 301 {
		return fmt.Errorf("country metadata: %w", err)
	}
	if _, err = subdivision.Parse("FI-18"); err != nil || len(subdivision.DatasetRecords()) != 5653 {
		return fmt.Errorf("subdivision metadata: %w", err)
	}
	languageCode, err := intlLanguage.Parse("fi")
	if err != nil || intlLanguage.Name(languageCode, textlanguage.English) != "Finnish" {
		return fmt.Errorf("language metadata: %w", err)
	}
	tag, err := locale.Parse("fi-FI-u-ca-gregory")
	if err != nil {
		return err
	}
	if _, err = tag.Canonical(); err != nil {
		return err
	}
	if _, err = currency.Parse("EUR"); err != nil || len(currency.DatasetRecords()) != 307 {
		return fmt.Errorf("currency metadata: %w", err)
	}
	number, err := phone.ParseE164("+358401234567")
	if err != nil {
		return err
	}
	if _, err = number.Format(phone.FormatInternational); err != nil {
		return err
	}
	code, err := postal.Parse("00100", finland)
	if err != nil {
		return err
	}
	_, err = code.Normalize(postal.NormalizeOptions{Case: postal.CaseUpperASCII})
	return err
}
