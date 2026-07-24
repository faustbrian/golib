package bip39_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
)

func FuzzMnemonicParsing(f *testing.F) {
	f.Add("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about")
	f.Add("一 一 一 一 一 一 一 一 一 一 一 一")
	f.Add("not a mnemonic")
	f.Fuzz(func(t *testing.T, phrase string) {
		if len(phrase) > 128<<10 {
			t.Skip()
		}
		mnemonic, err := bip39.Parse(phrase)
		if err != nil {
			return
		}
		roundTrip, err := bip39.ParseLanguage(mnemonic.String(), mnemonic.Language())
		if err != nil {
			t.Fatalf("ParseLanguage(round trip) error = %v", err)
		}
		if roundTrip.String() != mnemonic.String() {
			t.Fatal("mnemonic round trip changed canonical form")
		}
	})
}
