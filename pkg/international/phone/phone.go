// Package phone provides bounded, immutable libphonenumber-backed values. It
// makes no claim about ownership, reachability, identity, or delivery.
package phone

import (
	"strconv"
	"strings"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/nyaruka/phonenumbers"
)

const (
	// MaxBytes bounds phone parsing work, including separators and extensions.
	MaxBytes = 128
	// MaxExtensionBytes bounds extension storage and parsing work.
	MaxExtensionBytes = 20
)

// FormatStyle selects explicit display formatting.
type FormatStyle uint8

const (
	// FormatNational uses region-specific national display conventions.
	FormatNational FormatStyle = iota
	// FormatInternational includes the calling code and international spacing.
	FormatInternational
)

// NumberType is libphonenumber metadata, not an ownership or usage claim.
type NumberType uint8

const (
	// TypeUnknown means metadata cannot classify the number.
	TypeUnknown NumberType = iota
	// TypeFixedLine identifies a fixed-line numbering range.
	TypeFixedLine
	// TypeMobile identifies a mobile numbering range.
	TypeMobile
	// TypeFixedLineOrMobile means metadata cannot distinguish those classes.
	TypeFixedLineOrMobile
	// TypeTollFree identifies a toll-free numbering range.
	TypeTollFree
	// TypePremiumRate identifies a premium-rate numbering range.
	TypePremiumRate
	// TypeSharedCost identifies a shared-cost numbering range.
	TypeSharedCost
	// TypeVOIP identifies a voice-over-IP numbering range.
	TypeVOIP
	// TypePersonal identifies a personal-numbering range.
	TypePersonal
	// TypePager identifies a pager numbering range.
	TypePager
	// TypeUAN identifies a universal-access-number range.
	TypeUAN
	// TypeVoicemail identifies a voicemail access range.
	TypeVoicemail
)

// CallingCode is an immutable ITU country calling code. Its zero value is absent.
type CallingCode struct{ value uint16 }

// Number is an immutable snapshot of parsed number identity and metadata. Its
// zero value is absent. Default String formatting is intentionally redacted.
type Number struct {
	e164          string
	extension     string
	national      string
	international string
	region        country.Code
	callingCode   CallingCode
	numberType    NumberType
	possible      bool
	valid         bool
}

// ParseOptions supplies context without inferring it from environment or IP.
type ParseOptions struct {
	RegionHint country.Code
}

// Parse validates syntax and snapshots possible-versus-valid metadata. A
// successfully parsed number may still be possible but invalid.
func Parse(input string, options ParseOptions) (Number, error) {
	if len(input) > MaxBytes {
		return Number{}, international.ErrResourceLimit
	}
	if input == "" || !utf8.ValidString(input) {
		return Number{}, international.NewParseError("phone", "malformed input")
	}
	defaultRegion := "ZZ"
	if !options.RegionHint.IsZero() {
		defaultRegion = options.RegionHint.String()
	} else if !strings.HasPrefix(strings.TrimSpace(input), "+") {
		return Number{}, international.NewParseError("phone", "national input requires a region hint")
	}

	parsed, err := phonenumbers.Parse(input, defaultRegion)
	if err != nil {
		return Number{}, international.NewParseError("phone", "libphonenumber rejected input")
	}
	return snapshotParsed(parsed)
}

func snapshotParsed(parsed *phonenumbers.PhoneNumber) (Number, error) {
	extension := parsed.GetExtension()
	if len(extension) > MaxExtensionBytes || !decimal(extension) {
		return Number{}, international.NewParseError("phone", "invalid extension")
	}
	countryCallingCode := parsed.GetCountryCode()
	if countryCallingCode <= 0 || countryCallingCode > 999 {
		return Number{}, international.NewParseError("phone", "invalid calling code metadata")
	}

	region := country.Code{}
	regionText := phonenumbers.GetRegionCodeForNumber(parsed)
	if regionText != "" && regionText != "001" {
		region, _ = country.ParseWithOptions(regionText, country.ParseOptions{
			AllowHistoric: true, AllowReserved: true, AllowUserAssigned: true,
		})
	}

	return Number{
		e164:          phonenumbers.Format(parsed, phonenumbers.E164),
		extension:     extension,
		national:      phonenumbers.Format(parsed, phonenumbers.NATIONAL),
		international: phonenumbers.Format(parsed, phonenumbers.INTERNATIONAL),
		region:        region,
		callingCode:   CallingCode{value: uint16(countryCallingCode)},
		numberType:    mapNumberType(phonenumbers.GetNumberType(parsed)),
		possible:      phonenumbers.IsPossibleNumber(parsed),
		valid:         phonenumbers.IsValidNumber(parsed),
	}, nil
}

// ParseE164 accepts only a canonical E.164 identity without an extension.
func ParseE164(input string) (Number, error) {
	number, err := Parse(input, ParseOptions{})
	if err != nil {
		return Number{}, err
	}
	if input != number.e164 || number.extension != "" {
		return Number{}, international.NewParseError("phone", "input is not canonical E.164")
	}
	return number, nil
}

// ParseCallingCode accepts a supported plus-prefixed ITU calling code.
func ParseCallingCode(input string) (CallingCode, error) {
	if len(input) < 2 || len(input) > 4 || input[0] != '+' || !decimal(input[1:]) {
		return CallingCode{}, international.NewParseError("calling code", "malformed input")
	}
	value, _ := strconv.Atoi(input[1:])
	if value <= 0 || value > 999 {
		return CallingCode{}, international.NewParseError("calling code", "unsupported value")
	}
	if !phonenumbers.GetSupportedCallingCodes()[value] {
		return CallingCode{}, international.NewParseError("calling code", "unsupported value")
	}
	return CallingCode{value: uint16(value)}, nil
}

// E164 returns canonical number identity without an extension.
func (number Number) E164() string { return number.e164 }

// Extension returns the separately parsed extension.
func (number Number) Extension() string { return number.extension }

// Region returns reliable libphonenumber region metadata when available.
func (number Number) Region() country.Code { return number.region }

// CallingCode returns the parsed ITU calling code.
func (number Number) CallingCode() CallingCode { return number.callingCode }

// Type returns reliable libphonenumber number-type metadata.
func (number Number) Type() NumberType { return number.numberType }

// Possible reports whether the number has a possible numbering-plan shape.
func (number Number) Possible() bool { return number.possible }

// Valid reports whether current metadata recognizes the number as assigned-form valid.
func (number Number) Valid() bool { return number.valid }

// IsZero reports whether the number represents an absent value.
func (number Number) IsZero() bool { return number.e164 == "" }

// Format returns an explicit display form and never changes canonical identity.
func (number Number) Format(style FormatStyle) (string, error) {
	if number.IsZero() {
		return "", international.NewParseError("phone", "absent value")
	}
	switch style {
	case FormatNational:
		return number.national, nil
	case FormatInternational:
		return number.international, nil
	default:
		return "", international.NewParseError("phone format", "unknown style")
	}
}

// String returns a privacy-safe diagnostic instead of the personal value.
func (number Number) String() string { return "[phone]" }

// GoString returns a privacy-safe diagnostic instead of the personal value.
func (number Number) GoString() string { return "phone.Number{redacted}" }

// String returns the plus-prefixed calling code or empty string when absent.
func (code CallingCode) String() string {
	if code.IsZero() {
		return ""
	}
	return "+" + strconv.Itoa(int(code.value))
}

// Int returns the numeric calling code or zero when absent.
func (code CallingCode) Int() int { return int(code.value) }

// IsZero reports whether the calling code represents an absent value.
func (code CallingCode) IsZero() bool { return code.value == 0 }

func decimal(value string) bool {
	if value == "" {
		return true
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func mapNumberType(value phonenumbers.PhoneNumberType) NumberType {
	switch value {
	case phonenumbers.UNKNOWN:
		return TypeUnknown
	case phonenumbers.FIXED_LINE:
		return TypeFixedLine
	case phonenumbers.MOBILE:
		return TypeMobile
	case phonenumbers.FIXED_LINE_OR_MOBILE:
		return TypeFixedLineOrMobile
	case phonenumbers.TOLL_FREE:
		return TypeTollFree
	case phonenumbers.PREMIUM_RATE:
		return TypePremiumRate
	case phonenumbers.SHARED_COST:
		return TypeSharedCost
	case phonenumbers.VOIP:
		return TypeVOIP
	case phonenumbers.PERSONAL_NUMBER:
		return TypePersonal
	case phonenumbers.PAGER:
		return TypePager
	case phonenumbers.UAN:
		return TypeUAN
	case phonenumbers.VOICEMAIL:
		return TypeVoicemail
	default:
		return TypeUnknown
	}
}
