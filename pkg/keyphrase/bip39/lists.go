package bip39

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

const bip39Revision = "8c369ac8e60629ac6c032ffe21bb5ec5b35213d7"
const bip39Source = "https://raw.githubusercontent.com/bitcoin/bips/8c369ac8e60629ac6c032ffe21bb5ec5b35213d7/bip-0039/"

// Language identifies an official BIP-39 word list.
type Language string

const (
	// ChineseSimplified selects the official Simplified Chinese list.
	ChineseSimplified Language = "chinese_simplified"
	// ChineseTraditional selects the official Traditional Chinese list.
	ChineseTraditional Language = "chinese_traditional"
	// Czech selects the official Czech list.
	Czech Language = "czech"
	// English selects the official English list.
	English Language = "english"
	// French selects the official French list.
	French Language = "french"
	// Italian selects the official Italian list.
	Italian Language = "italian"
	// Japanese selects the official Japanese list.
	Japanese Language = "japanese"
	// Korean selects the official Korean list.
	Korean Language = "korean"
	// Portuguese selects the official Portuguese list.
	Portuguese Language = "portuguese"
	// Spanish selects the official Spanish list.
	Spanish Language = "spanish"
)

var officialLanguages = []Language{
	ChineseSimplified,
	ChineseTraditional,
	Czech,
	English,
	French,
	Italian,
	Japanese,
	Korean,
	Portuguese,
	Spanish,
}

var checksums = map[Language]string{
	ChineseSimplified:  "5c5942792bd8340cb8b27cd592f1015edf56a8c5b26276ee18a482428e7c5726",
	ChineseTraditional: "417b26b3d8500a4ae3d59717d7011952db6fc2fb84b807f3f94ac734e89c1b5f",
	Czech:              "7e80e161c3e93d9554c2efb78d4e3cebf8fc727e9c52e03b83b94406bdcc95fc",
	English:            "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda",
	French:             "ebc3959ab7801a1df6bac4fa7d970652f1df76b683cd2f4003c941c63d517e59",
	Italian:            "d392c49fdb700a24cd1fceb237c1f65dcc128f6b34a8aacb58b59384b5c648c2",
	Japanese:           "2eed0aef492291e061633d7ad8117f1a2b03eb80a29d0e4e3117ac2528d05ffd",
	Korean:             "9e95f86c167de88f450f0aaf89e87f6624a57f973c67b516e338e8e8b8897f60",
	Portuguese:         "2685e9c194c82ae67e10ba59d9ea5345a23dc093e92276fc5361f6667d79cd3f",
	Spanish:            "46846a5a0139d1e3cb77293e521c2865f7bcdb82c44e8d0a06a2cd0ecba48c0b",
}

//go:embed data/*.txt
var embeddedLists embed.FS

var (
	listsOnce sync.Once
	lists     map[Language]*wordlist.List
	listsErr  error
)

// Languages returns all official languages in stable lexical order.
func Languages() []Language {
	return append([]Language(nil), officialLanguages...)
}

// List returns an immutable, validated official list.
func List(language Language) (*wordlist.List, error) {
	if _, exists := checksums[language]; !exists {
		return nil, &Error{Code: CodeUnsupportedLanguage}
	}
	listsOnce.Do(loadLists)
	return resolveList(language, lists, listsErr)
}

func loadLists() {
	lists, listsErr = loadAll(embeddedLists)
}

func loadAll(files fs.FS) (map[Language]*wordlist.List, error) {
	loaded := make(map[Language]*wordlist.List, len(officialLanguages))
	for _, language := range officialLanguages {
		list, err := loadOne(files, language, checksums[language])
		if err != nil {
			return nil, err
		}
		loaded[language] = list
	}
	return loaded, nil
}

func loadOne(files fs.FS, language Language, checksum string) (*wordlist.List, error) {
	filename := string(language) + ".txt"
	encoded, err := fs.ReadFile(files, "data/"+filename)
	if err != nil {
		return nil, &Error{Code: CodeListIntegrity, Cause: err}
	}
	words := strings.Split(strings.TrimSuffix(string(encoded), "\n"), "\n")
	metadata := wordlist.Metadata{
		ID:            "bip39-" + string(language),
		Language:      string(language),
		Source:        bip39Source + filename,
		Version:       bip39Revision,
		License:       "MIT",
		ExpectedWords: 2048,
		SHA256:        checksum,
		SourceSHA256:  checksum,
	}
	list, err := wordlist.New(metadata, words, wordlist.WithNFKD(), wordlist.WithUniquePrefix(4))
	if err != nil {
		return nil, &Error{Code: CodeListIntegrity, Cause: fmt.Errorf("%s: %w", language, err)}
	}
	return list, nil
}

func resolveList(language Language, available map[Language]*wordlist.List, loadErr error) (*wordlist.List, error) {
	if loadErr != nil {
		return nil, loadErr
	}
	return available[language], nil
}
