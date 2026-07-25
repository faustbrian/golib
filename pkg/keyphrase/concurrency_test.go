package keyphrase_test

import (
	"context"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
	"github.com/faustbrian/golib/pkg/keyphrase/passphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist/eff"
)

func TestSharedGeneratorsAndImmutableListsAreRaceSafe(t *testing.T) {
	t.Parallel()

	list, err := eff.ShortOne()
	if err != nil {
		t.Fatalf("eff.ShortOne() error = %v", err)
	}
	passwordGenerator := password.DefaultGenerator()
	passphraseGenerator := passphrase.DefaultGenerator()
	passwordPolicy := password.Policy{Length: 16, Alphabet: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}
	passphrasePolicy := passphrase.Policy{WordList: list, Words: 4, Separator: " "}
	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.English)
	if err != nil {
		t.Fatalf("bip39.FromEntropy() error = %v", err)
	}

	var waitGroup sync.WaitGroup
	for range 16 {
		waitGroup.Go(func() {
			for range 25 {
				secret, generationErr := passwordGenerator.Generate(context.Background(), passwordPolicy)
				if generationErr != nil {
					t.Errorf("password.Generate() error = %v", generationErr)
					return
				}
				clear(secret)
				phrase, generationErr := passphraseGenerator.Generate(context.Background(), passphrasePolicy)
				if generationErr != nil {
					t.Errorf("passphrase.Generate() error = %v", generationErr)
					return
				}
				clear(phrase)
				if _, parseErr := bip39.ParseLanguage(mnemonic.String(), bip39.English); parseErr != nil {
					t.Errorf("bip39.ParseLanguage() error = %v", parseErr)
					return
				}
				if word, ok := list.Word(0); !ok || word == "" {
					t.Error("immutable list read failed")
					return
				}
			}
		})
	}
	waitGroup.Wait()
}
