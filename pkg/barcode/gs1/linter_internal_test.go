package gs1

import "testing"

func TestDictionaryLinterDispatch(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "csum", value: "09501101530003", want: true},
		{name: "csum", value: "09501101530004"},
		{name: "yymmd0", value: "240200", want: true},
		{name: "yymmd0", value: "241300"},
		{name: "yymmdd", value: "240229", want: true},
		{name: "yymmdd", value: "230229"},
		{name: "yyyymmdd", value: "20240229", want: true},
		{name: "yyyymmdd", value: "20240001"},
		{name: "hh", value: "23", want: true},
		{name: "hh", value: "24"},
		{name: "mi", value: "59", want: true},
		{name: "ss", value: "60"},
		{name: "hhmi", value: "2359", want: true},
		{name: "hhmi", value: "2460"},
		{name: "hyphen", value: "---", want: true},
		{name: "hyphen", value: "-A"},
		{name: "hasnondigit", value: "1A", want: true},
		{name: "hasnondigit", value: "12"},
		{name: "nonzero", value: "001", want: true},
		{name: "nonzero", value: "000"},
		{name: "zero", value: "000", want: true},
		{name: "zero", value: "001"},
		{name: "nozeroprefix", value: "10", want: true},
		{name: "nozeroprefix", value: "01"},
		{name: "yesno", value: "1", want: true},
		{name: "yesno", value: "9"},
		{name: "winding", value: "9", want: true},
		{name: "winding", value: "2"},
		{name: "iso5218", value: "2", want: true},
		{name: "iso5218", value: "3"},
		{name: "latitude", value: "1800000000", want: true},
		{name: "latitude", value: "1800000001"},
		{name: "longitude", value: "3600000000", want: true},
		{name: "longitude", value: "3600000001"},
		{name: "pcenc", value: "ABC%20", want: true},
		{name: "pcenc", value: "ABC%2"},
		{name: "pieceoftotal", value: "0203", want: true},
		{name: "pieceoftotal", value: "0302"},
		{name: "posinseqslash", value: "9/10", want: true},
		{name: "posinseqslash", value: "10/9"},
		{name: "importeridx", value: "_", want: true},
		{name: "importeridx", value: "."},
		{name: "mediatype", value: "10", want: true},
		{name: "mediatype", value: "11"},
		{name: "iban", value: "GB82WEST12345698765432", want: true},
		{name: "iban", value: "GB82WEST12345698765433"},
		{name: "csumalpha", value: "1987654Ad4X4bL5ttr2310c2K", want: true},
		{name: "csumalpha", value: "1987654Ad4X4bL5ttr2310cXK"},
		{name: "iso3166", value: "246", want: true},
		{name: "iso3166", value: "999"},
		{name: "iso3166999", value: "999", want: true},
		{name: "iso3166999", value: "998"},
		{name: "iso3166alpha2", value: "FI", want: true},
		{name: "iso3166alpha2", value: "XX"},
		{name: "iso4217", value: "978", want: true},
		{name: "iso4217", value: "000"},
		{name: "packagetype", value: "BX", want: true},
		{name: "packagetype", value: "BAD"},
		{name: "couponposoffer", value: "001234561234560123456", want: true},
		{name: "couponposoffer", value: "201234561234560123456"},
		{name: "gcppos1", value: "anything", want: true},
		{name: "couponcode", value: "012345612345611110123", want: true},
		{name: "couponcode", value: "0123456123456111101237"},
		{name: "unknown", value: "anything"},
	}
	for _, test := range tests {
		if got := validateLinter(test.name, test.value); got != test.want {
			t.Fatalf("validateLinter(%q, %q) = %t, want %t", test.name, test.value, got, test.want)
		}
	}
}

func TestLinterHelperMalformedInputs(t *testing.T) {
	if validDate("", 2, false) || validDate("24AA01", 2, false) ||
		validRange("A", 0, 10) || validMaximum("A", 1, 10) ||
		validPercentEncoding("%GG") || validPieceOfTotal("") ||
		validPieceOfTotal("010") || validPieceOfTotal("AA") ||
		validPositionInSequence("1") || validPositionInSequence("0/1") ||
		validIBAN("bad") || validIBAN("GB82west12345698765432") ||
		validNumericAllocation("FI", iso3166Codes) ||
		validAlpha2Allocation("fi") || validAlphaCheckPair("") ||
		validAlphaCheckPair("!ZZ") || validCouponPositiveOffer("") ||
		validPackageType(" ") ||
		allDigits("") || allDigits("1A") || !allDigits("12") ||
		!isHex('f') || isHex('x') {
		t.Fatal("malformed helper input accepted")
	}
}

func TestAlphaCheckPairBoundaries(t *testing.T) {
	if !validAlphaCheckPair("22") || validAlphaCheckPair("33") ||
		validAlphaCheckPair(" 22") || validAlphaCheckPair(string(make([]byte, 100))) {
		t.Fatal("alphanumeric check-pair boundary accepted incorrectly")
	}
}

func TestCouponPositiveOfferBoundaries(t *testing.T) {
	for _, value := range []string{
		"", "00", "07123456", "00123456123456", "001234561234560",
		"0012345612345601234567",
	} {
		if validCouponPositiveOffer(value) {
			t.Fatalf("validCouponPositiveOffer(%q) accepted malformed data", value)
		}
	}
}

func TestCouponCodeGrammar(t *testing.T) {
	const base = "012345612345611110123"
	valid := []string{
		base,
		base + "101101239",
		base + "21101239",
		base + "3251231",
		base + "4250101",
		base + "50123456",
		base + "611234567",
		base + "90071",
		base + "10110123921101239325123142501015012345661123456790071",
	}
	for _, value := range valid {
		if !validCouponCode(value) {
			t.Fatalf("validCouponCode(%q) rejected valid data", value)
		}
	}

	invalid := []string{
		"", "A", "0", "0123456", "0123456123456", "01234561234560",
		"01234561234561", "012345612345611", "01234561234561111",
		"012345612345611116", "01234561234561111012",
		base + "1", base + "14", base + "101", base + "1016",
		base + "10110123", base + "101101237", base + "101101230",
		base + "2", base + "21", base + "2116", base + "2110123",
		base + "21101237",
		base + "3", base + "3251332", base + "4250101" + "3",
		base + "4", base + "4251332", base + "3250101" + "4250201",
		base + "5", base + "50", base + "5012345",
		base + "6", base + "60", base + "61123456",
		base + "9", base + "900", base + "93071", base + "90371",
		base + "90072", base + "7",
	}
	for _, value := range invalid {
		if validCouponCode(value) {
			t.Fatalf("validCouponCode(%q) accepted malformed data", value)
		}
	}
}
