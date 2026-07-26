// Package localized provides immutable values keyed by canonical BCP 47 tags.
package localized

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"iter"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/international/locale"
)

// Limits bounds construction work and retained content.
type Limits struct {
	MaxLocales    int
	MaxTagBytes   int
	MaxTextBytes  int
	MaxTotalBytes int
}

// DuplicatePolicy selects construction behavior after locale canonicalization.
type DuplicatePolicy uint8

const (
	// RejectDuplicates fails construction when canonical identities repeat.
	RejectDuplicates DuplicatePolicy = iota
	// FirstWins preserves the first canonical occurrence.
	FirstWins
	// LastWins preserves the final canonical occurrence.
	LastWins
)

// ConstructionOptions configures bounded construction behavior.
type ConstructionOptions struct {
	Limits     Limits
	Duplicates DuplicatePolicy
	Locales    LocalePolicy
}

// LocalePolicy explicitly rejects selected otherwise-valid BCP 47 classes.
// Its zero value accepts und, mul, and private-use tags.
type LocalePolicy struct {
	RejectUnd        bool
	RejectMul        bool
	RejectPrivateUse bool
}

const (
	defaultMaxLocales    = 128
	defaultMaxTagBytes   = 255
	defaultMaxTextBytes  = 1 << 20
	defaultMaxTotalBytes = 8 << 20
)

// DefaultLimits returns conservative production defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxLocales: defaultMaxLocales, MaxTagBytes: defaultMaxTagBytes,
		MaxTextBytes: defaultMaxTextBytes, MaxTotalBytes: defaultMaxTotalBytes,
	}
}

// Entry associates text with a BCP 47 locale.
type Entry struct {
	Locale locale.Tag
	Text   string
}

// Text is an immutable collection of UTF-8 strings keyed by locale. Its zero
// value is an empty collection and is safe for concurrent reads.
type Text struct {
	entries []Entry
	index   map[string]int
}

// NewText constructs Text, rejecting duplicate canonical locale identities.
func NewText(entries ...Entry) (Text, error) {
	return NewTextWithOptions(ConstructionOptions{}, entries...)
}

// NewTextWithLimits constructs Text using explicit resource limits.
func NewTextWithLimits(limits Limits, entries ...Entry) (Text, error) {
	return NewTextWithOptions(ConstructionOptions{Limits: limits}, entries...)
}

// NewTextWithOptions constructs Text with explicit duplicate and limit policy.
func NewTextWithOptions(options ConstructionOptions, entries ...Entry) (Text, error) {
	if options.Duplicates > LastWins {
		return Text{}, ErrInvalidPolicy
	}
	limits := options.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if limits.MaxLocales < 0 || limits.MaxTagBytes < 0 ||
		limits.MaxTextBytes < 0 || limits.MaxTotalBytes < 0 {
		return Text{}, fmt.Errorf("%w: negative limit", ErrLimitExceeded)
	}
	if len(entries) == 0 {
		return Text{}, nil
	}
	if len(entries) > limits.MaxLocales {
		return Text{}, fmt.Errorf("%w: locale count", ErrLimitExceeded)
	}

	byLocale := make(map[string]Entry, len(entries))
	total := 0
	for _, entry := range entries {
		canonical, err := entry.Locale.Canonical()
		if err != nil {
			return Text{}, ErrInvalidLocale
		}
		entry.Locale = canonical
		key := canonical.String()
		if (options.Locales.RejectUnd && (key == "und" || strings.HasPrefix(key, "und-"))) ||
			(options.Locales.RejectMul && (key == "mul" || strings.HasPrefix(key, "mul-"))) ||
			(options.Locales.RejectPrivateUse && canonical.HasPrivateUse()) {
			return Text{}, ErrLocaleRejected
		}
		if len(key) > limits.MaxTagBytes {
			return Text{}, fmt.Errorf("%w: tag bytes", ErrLimitExceeded)
		}
		if !utf8.ValidString(entry.Text) {
			return Text{}, ErrInvalidUTF8
		}
		if len(entry.Text) > limits.MaxTextBytes {
			return Text{}, fmt.Errorf("%w: text bytes", ErrLimitExceeded)
		}
		total += len(entry.Text)
		if total > limits.MaxTotalBytes {
			return Text{}, fmt.Errorf("%w: total text bytes", ErrLimitExceeded)
		}
		if _, exists := byLocale[key]; exists {
			switch options.Duplicates {
			case RejectDuplicates:
				return Text{}, ErrDuplicateLocale
			case FirstWins:
				continue
			}
		}
		byLocale[key] = entry
	}
	owned := make([]Entry, 0, len(byLocale))
	for _, entry := range byLocale {
		owned = append(owned, entry)
	}
	sort.Slice(owned, func(i, j int) bool {
		return owned[i].Locale.String() < owned[j].Locale.String()
	})
	index := make(map[string]int, len(owned))
	for i := range owned {
		index[owned[i].Locale.String()] = i
	}

	return Text{entries: owned, index: index}, nil
}

// TextFromMap parses locale keys and constructs an owned Text value.
func TextFromMap(values map[string]string) (Text, error) {
	entries := make([]Entry, 0, len(values))
	for raw, value := range values {
		if strings.ContainsAny(raw, "_ \t\r\n") {
			return Text{}, ErrInvalidLocale
		}
		if len(raw) > defaultMaxTagBytes {
			return Text{}, fmt.Errorf("%w: tag bytes", ErrLimitExceeded)
		}
		tag, err := locale.Parse(raw)
		if err != nil {
			return Text{}, ErrInvalidLocale
		}
		entries = append(entries, Entry{Locale: tag, Text: value})
	}
	return NewText(entries...)
}

// Len returns the number of present locales, including present-empty values.
func (t Text) Len() int { return len(t.entries) }

// IsEmpty reports whether no locales are present.
func (t Text) IsEmpty() bool { return len(t.entries) == 0 }

// Has reports whether locale is present. It performs no matching or fallback.
func (t Text) Has(tag locale.Tag) bool {
	_, ok := t.canonicalIndex(tag)
	return ok
}

// Get returns the exact locale value and whether it is present. It performs no
// matching or fallback, so an empty string with true is distinct from missing.
func (t Text) Get(tag locale.Tag) (string, bool) {
	i, ok := t.canonicalIndex(tag)
	if !ok {
		return "", false
	}
	return t.entries[i].Text, true
}

func (t Text) canonicalIndex(tag locale.Tag) (int, bool) {
	canonical, err := tag.Canonical()
	if err != nil {
		return 0, false
	}
	i, ok := t.index[canonical.String()]
	return i, ok
}

// Require returns the exact locale value or ErrMissingLocale.
func (t Text) Require(tag locale.Tag) (string, error) {
	value, ok := t.Get(tag)
	if !ok {
		return "", ErrMissingLocale
	}
	return value, nil
}

// Locales returns canonical locales in deterministic lexical order. The caller
// owns the returned slice.
func (t Text) Locales() []locale.Tag {
	locales := make([]locale.Tag, len(t.entries))
	for i := range t.entries {
		locales[i] = t.entries[i].Locale
	}
	return locales
}

// Entries returns a deterministic caller-owned copy of all entries.
func (t Text) Entries() []Entry { return append([]Entry(nil), t.entries...) }

// Pair is a canonical string-keyed construction and wire convenience.
type Pair struct {
	Locale string
	Text   string
}

// TextFromPairs parses strict BCP 47 pair keys and constructs an owned Text.
func TextFromPairs(pairs ...Pair) (Text, error) {
	return TextFromPairsWithOptions(ConstructionOptions{}, pairs...)
}

// TextFromPairsWithOptions parses strict pair keys and applies explicit
// construction, duplicate, locale-acceptance, and resource policies.
func TextFromPairsWithOptions(options ConstructionOptions, pairs ...Pair) (Text, error) {
	entries := make([]Entry, 0, len(pairs))
	for _, pair := range pairs {
		if strings.ContainsAny(pair.Locale, "_ \t\r\n") {
			return Text{}, ErrInvalidLocale
		}
		if len(pair.Locale) > defaultMaxTagBytes {
			return Text{}, fmt.Errorf("%w: tag bytes", ErrLimitExceeded)
		}
		tag, err := locale.Parse(pair.Locale)
		if err != nil {
			return Text{}, ErrInvalidLocale
		}
		entries = append(entries, Entry{Locale: tag, Text: pair.Text})
	}
	return NewTextWithOptions(options, entries...)
}

// Pairs returns deterministic caller-owned canonical string pairs.
func (t Text) Pairs() []Pair {
	pairs := make([]Pair, len(t.entries))
	for i, entry := range t.entries {
		pairs[i] = Pair{Locale: entry.Locale.String(), Text: entry.Text}
	}
	return pairs
}

// All returns a deterministic iterator over immutable locale and text values.
func (t Text) All() iter.Seq2[locale.Tag, string] {
	return func(yield func(locale.Tag, string) bool) {
		for _, entry := range t.entries {
			if !yield(entry.Locale, entry.Text) {
				return
			}
		}
	}
}

// Builder collects entries and copies them into an immutable Text on Build.
type Builder struct {
	options ConstructionOptions
	entries []Entry
}

// NewBuilder creates an isolated construction builder.
func NewBuilder(options ConstructionOptions) *Builder {
	return &Builder{options: options}
}

// Add appends a typed locale pair to the builder. A nil builder is a no-op.
func (b *Builder) Add(tag locale.Tag, text string) {
	if b == nil {
		return
	}
	b.entries = append(b.entries, Entry{Locale: tag, Text: text})
}

// AddString parses a strict BCP 47 key and appends it when valid.
func (b *Builder) AddString(rawLocale, text string) error {
	if b == nil {
		return ErrInvalidPolicy
	}
	if strings.ContainsAny(rawLocale, "_ \t\r\n") {
		return ErrInvalidLocale
	}
	tag, err := locale.Parse(rawLocale)
	if err != nil {
		return ErrInvalidLocale
	}
	b.Add(tag, text)
	return nil
}

// Build validates and copies the builder's current entries.
func (b *Builder) Build() (Text, error) {
	if b == nil {
		return Text{}, nil
	}
	return NewTextWithOptions(b.options, b.entries...)
}

// Set returns a copy with locale added or replaced.
func (t Text) Set(tag locale.Tag, value string) (Text, error) {
	entries := t.Entries()
	canonical, err := tag.Canonical()
	if err != nil {
		return Text{}, ErrInvalidLocale
	}
	if i, ok := t.index[canonical.String()]; ok {
		entries[i].Text = value
	} else {
		entries = append(entries, Entry{Locale: canonical, Text: value})
	}
	return NewText(entries...)
}

// Remove returns a copy without locale. Removing an absent locale is a no-op.
func (t Text) Remove(tag locale.Tag) Text {
	i, ok := t.canonicalIndex(tag)
	if !ok {
		return t
	}
	entries := make([]Entry, 0, len(t.entries)-1)
	entries = append(entries, t.entries[:i]...)
	entries = append(entries, t.entries[i+1:]...)
	result, _ := NewText(entries...)
	return result
}

// Filter returns entries accepted by predicate in deterministic order. A nil
// predicate returns the receiver unchanged.
func (t Text) Filter(predicate func(locale.Tag, string) bool) Text {
	if predicate == nil {
		return t
	}
	entries := make([]Entry, 0, len(t.entries))
	for _, entry := range t.entries {
		if predicate(entry.Locale, entry.Text) {
			entries = append(entries, entry)
		}
	}
	result, _ := NewText(entries...)
	return result
}

// Map returns a copy with every text transformed transactionally.
func (t Text) Map(transform func(locale.Tag, string) (string, error)) (Text, error) {
	if transform == nil {
		return Text{}, ErrInvalidPolicy
	}
	entries := t.Entries()
	for i := range entries {
		text, err := transform(entries[i].Locale, entries[i].Text)
		if err != nil {
			return Text{}, err
		}
		entries[i].Text = text
	}
	return NewText(entries...)
}

// Equal reports structural equality of locale and text pairs.
func (t Text) Equal(other Text) bool {
	if len(t.entries) != len(other.entries) {
		return false
	}
	for i := range t.entries {
		if t.entries[i].Locale.String() != other.entries[i].Locale.String() ||
			t.entries[i].Text != other.entries[i].Text {
			return false
		}
	}
	return true
}

// Hash returns a stable, length-framed SHA-256 digest of canonical entries.
func (t Text) Hash() [sha256.Size]byte {
	hash := sha256.New()
	var size [8]byte
	for _, entry := range t.entries {
		for _, value := range []string{entry.Locale.String(), entry.Text} {
			binary.BigEndian.PutUint64(size[:], uint64(len(value)))
			_, _ = hash.Write(size[:])
			_, _ = hash.Write([]byte(value))
		}
	}
	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	return sum
}

// MergePolicy selects behavior for locales present in both values.
type MergePolicy uint8

const (
	// LeftWins preserves the receiver on conflicts.
	LeftWins MergePolicy = iota
	// RightWins replaces conflicts with the overlay value.
	RightWins
	// RejectConflict fails if any locale is present in both values.
	RejectConflict
	// ResolveConflict delegates overlapping values to a resolver.
	ResolveConflict
)

// EmptyPolicy determines whether present-empty participates in merging.
type EmptyPolicy uint8

const (
	// EmptyIsValue treats an empty string as a present conflicting value.
	EmptyIsValue EmptyPolicy = iota
	// EmptyIsAbsent omits empty strings before conflict resolution.
	EmptyIsAbsent
)

// MergeResolver returns the text to retain for one canonical conflict.
type MergeResolver func(locale locale.Tag, left, right string) (string, error)

// MergeOptions configures explicit conflict, empty, and output bounds.
type MergeOptions struct {
	Conflicts MergePolicy
	Empty     EmptyPolicy
	Resolver  MergeResolver
	Limits    Limits
}

// Merge combines two values using an explicit conflict policy.
func (t Text) Merge(other Text, policy MergePolicy) (Text, error) {
	return t.MergeWithOptions(other, MergeOptions{Conflicts: policy})
}

// MergeWithOptions combines values using named conflict and empty semantics.
func (t Text) MergeWithOptions(other Text, options MergeOptions) (Text, error) {
	if options.Conflicts > ResolveConflict || options.Empty > EmptyIsAbsent {
		return Text{}, ErrInvalidPolicy
	}
	if options.Conflicts == ResolveConflict && options.Resolver == nil {
		return Text{}, ErrResolverRequired
	}
	entries := make(map[string]Entry, t.Len()+other.Len())
	for _, entry := range t.entries {
		if options.Empty == EmptyIsAbsent && entry.Text == "" {
			continue
		}
		entries[entry.Locale.String()] = entry
	}
	for _, entry := range other.entries {
		if options.Empty == EmptyIsAbsent && entry.Text == "" {
			continue
		}
		key := entry.Locale.String()
		left, conflict := entries[key]
		if conflict {
			switch options.Conflicts {
			case LeftWins:
				continue
			case RejectConflict:
				return Text{}, ErrConflict
			case ResolveConflict:
				text, err := options.Resolver(entry.Locale, left.Text, entry.Text)
				if err != nil {
					return Text{}, err
				}
				entry.Text = text
			}
		}
		entries[key] = entry
	}
	merged := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		merged = append(merged, entry)
	}
	limits := options.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	result, err := NewTextWithLimits(limits, merged...)
	if err != nil {
		return Text{}, err
	}
	return result, nil
}
