package httpsignature

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRFC9421AppendixB2Examples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		section        string
		inputField     string
		signatureField string
		message        func() MessageContext
		base           string
		algorithm      Algorithm
		key            func(*testing.T) any
	}{
		{
			section:        "B.2.1 minimal rsa-pss-sha512 signature",
			inputField:     `sig-b21=();created=1618884473;keyid="test-key-rsa-pss";nonce="b3k2pp5k7z-50gnwp.yemd"`,
			signatureField: `sig-b21=:d2pmTvmbncD3xQm8E9ZV2828BjQWGgiwAaw5bAkgibUopemLJcWDy/lkbbHAve4cRAtx31Iq786U7it++wgGxbtRxf8Udx7zFZsckzXaJMkA7ChG52eSkFxykJeNqsrWH5S+oxNFlD4dzVuwe8DhTSja8xxbR/Z2cOGdCbzR72rgFWhzx2VjBqJzsPLMIQKhO4DGezXehhWwE56YCE+O6c0mKZsfxVrogUvA4HELjVKWmAvtl6UnCh8jYzuVG5WSb/QEVPnP5TmcAnLH1g+s++v6d4s8m0gCw1fV5/SITLq9mhho8K3+7EPYTU8IU1bLhdxO5Nyt8C8ssinQ98Xw9Q==:`,
			message:        rfc9421TestRequest,
			base:           `"@signature-params": ();created=1618884473;keyid="test-key-rsa-pss";nonce="b3k2pp5k7z-50gnwp.yemd"`,
			algorithm:      RSAPSSSHA512,
			key:            func(t *testing.T) any { return rfc9421RSAPSSPublicKey(t) },
		},
		{
			section:        "B.2.2 selective covered components using rsa-pss-sha512",
			inputField:     `sig-b22=("@authority" "content-digest" "@query-param";name="Pet");created=1618884473;keyid="test-key-rsa-pss";tag="header-example"`,
			signatureField: `sig-b22=:LjbtqUbfmvjj5C5kr1Ugj4PmLYvx9wVjZvD9GsTT4F7GrcQEdJzgI9qHxICagShLRiLMlAJjtq6N4CDfKtjvuJyE5qH7KT8UCMkSowOB4+ECxCmT8rtAmj/0PIXxi0A0nxKyB09RNrCQibbUjsLS/2YyFYXEu4TRJQzRw1rLEuEfY17SARYhpTlaqwZVtR8NV7+4UKkjqpcAoFqWFQh62s7Cl+H2fjBSpqfZUJcsIk4N6wiKYd4je2U/lankenQ99PZfB4jY3I5rSV2DSBVkSFsURIjYErOs0tFTQosMTAoxk//0RoKUqiYY8Bh0aaUEb0rQl3/XaVe4bXTugEjHSw==:`,
			message:        rfc9421TestRequest,
			base: `"@authority": example.com
"content-digest": sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:
"@query-param";name="Pet": dog
"@signature-params": ("@authority" "content-digest" "@query-param";name="Pet");created=1618884473;keyid="test-key-rsa-pss";tag="header-example"`,
			algorithm: RSAPSSSHA512,
			key:       func(t *testing.T) any { return rfc9421RSAPSSPublicKey(t) },
		},
		{
			section:        "B.2.3 full coverage using rsa-pss-sha512",
			inputField:     `sig-b23=("date" "@method" "@path" "@query" "@authority" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-rsa-pss"`,
			signatureField: `sig-b23=:bbN8oArOxYoyylQQUU6QYwrTuaxLwjAC9fbY2F6SVWvh0yBiMIRGOnMYwZ/5MR6fb0Kh1rIRASVxFkeGt683+qRpRRU5p2voTp768ZrCUb38K0fUxN0O0iC59DzYx8DFll5GmydPxSmme9v6ULbMFkl+V5B1TP/yPViV7KsLNmvKiLJH1pFkh/aYA2HXXZzNBXmIkoQoLd7YfW91kE9o/CCoC1xMy7JA1ipwvKvfrs65ldmlu9bpG6A9BmzhuzF8Eim5f8ui9eH8LZH896+QIF61ka39VBrohr9iyMUJpvRX2Zbhl5ZJzSRxpJyoEZAFL2FUo5fTIztsDZKEgM4cUA==:`,
			message:        rfc9421TestRequest,
			base: `"date": Tue, 20 Apr 2021 02:07:55 GMT
"@method": POST
"@path": /foo
"@query": ?param=Value&Pet=dog
"@authority": example.com
"content-type": application/json
"content-digest": sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:
"content-length": 18
"@signature-params": ("date" "@method" "@path" "@query" "@authority" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-rsa-pss"`,
			algorithm: RSAPSSSHA512,
			key:       func(t *testing.T) any { return rfc9421RSAPSSPublicKey(t) },
		},
		{
			section:        "B.2.4 response using ecdsa-p256-sha256",
			inputField:     `sig-b24=("@status" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-ecc-p256"`,
			signatureField: `sig-b24=:wNmSUAhwb5LxtOtOpNa6W5xj067m5hFrj0XQ4fvpaCLx0NKocgPquLgyahnzDnDAUy5eCdlYUEkLIj+32oiasw==:`,
			message:        rfc9421TestResponse,
			base: `"@status": 200
"content-type": application/json
"content-digest": sha-512=:mEWXIS7MaLRuGgxOBdODa3xqM1XdEvxoYhvlCFJ41QJgJc4GTsPp29l5oGX69wWdXymyU0rjJuahq4l5aGgfLQ==:
"content-length": 23
"@signature-params": ("@status" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-ecc-p256"`,
			algorithm: ECDSAP256SHA256,
			key:       func(t *testing.T) any { return rfc9421P256PublicKey(t) },
		},
		{
			section:        "B.2.5 request using hmac-sha256",
			inputField:     `sig-b25=("date" "@authority" "content-type");created=1618884473;keyid="test-shared-secret"`,
			signatureField: `sig-b25=:pxcQw6G3AjtMBQjwo8XzkZf/bws5LelbaMk5rGIGtE8=:`,
			message:        rfc9421TestRequest,
			base: `"date": Tue, 20 Apr 2021 02:07:55 GMT
"@authority": example.com
"content-type": application/json
"@signature-params": ("date" "@authority" "content-type");created=1618884473;keyid="test-shared-secret"`,
			algorithm: HMACSHA256,
			key:       func(t *testing.T) any { return rfc9421HMACKey(t) },
		},
		{
			section:        "B.2.6 request using ed25519",
			inputField:     `sig-b26=("date" "@method" "@path" "@authority" "content-type" "content-length");created=1618884473;keyid="test-key-ed25519"`,
			signatureField: `sig-b26=:wqcAqbmYJ2ji2glfAMaRy4gruYYnx2nEFN2HN6jrnDnQCK1u02Gb04v9EDgwUPiu4A0w6vuQv5lIp5WPpBKRCw==:`,
			message:        rfc9421TestRequest,
			base: `"date": Tue, 20 Apr 2021 02:07:55 GMT
"@method": POST
"@path": /foo
"@authority": example.com
"content-type": application/json
"content-length": 18
"@signature-params": ("date" "@method" "@path" "@authority" "content-type" "content-length");created=1618884473;keyid="test-key-ed25519"`,
			algorithm: Ed25519,
			key: func(t *testing.T) any {
				return ed25519.PublicKey(decodeRawURL(t, "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs"))
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.section, func(t *testing.T) {
			t.Parallel()

			inputs, err := ParseSignatureInputs([]string{test.inputField})
			if err != nil {
				t.Fatalf("ParseSignatureInputs(RFC 9421 Appendix %s) error = %v", test.section, err)
			}
			if got := inputs.String(); got != test.inputField {
				t.Fatalf("Signature-Input canonical form = %q, want %q", got, test.inputField)
			}
			signatures, err := ParseSignatures([]string{test.signatureField})
			if err != nil {
				t.Fatalf("ParseSignatures(RFC 9421 Appendix %s) error = %v", test.section, err)
			}
			if got := signatures.String(); got != test.signatureField {
				t.Fatalf("Signature canonical form = %q, want %q", got, test.signatureField)
			}

			inputEntries, signatureEntries := inputs.Entries(), signatures.Entries()
			if len(inputEntries) != 1 || len(signatureEntries) != 1 || inputEntries[0].Label != signatureEntries[0].Label {
				t.Fatalf("parsed fields = %#v, %#v", inputEntries, signatureEntries)
			}
			base, err := CreateSignatureBase(test.message(), inputEntries[0])
			if err != nil {
				t.Fatalf("CreateSignatureBase(RFC 9421 Appendix %s) error = %v", test.section, err)
			}
			if base != test.base {
				t.Fatalf("signature base =\n%s\nwant =\n%s", base, test.base)
			}
			if err := Verify(context.Background(), test.algorithm, test.key(t), []byte(base), signatureEntries[0].Value); err != nil {
				t.Fatalf("Verify(RFC 9421 Appendix %s) error = %v", test.section, err)
			}
		})
	}
}

func TestRFC9421AppendixB3TLSTerminatingProxyExample(t *testing.T) {
	t.Parallel()

	const inputField = `ttrp=("@path" "@query" "@method" "@authority" "client-cert");created=1618884473;keyid="test-key-ecc-p256"`
	const signatureField = `ttrp=:xVMHVpawaAC/0SbHrKRs9i8I3eOs5RtTMGCWXm/9nvZzoHsIg6Mce9315T6xoklyy0yzhD9ah4JHRwMLOgmizw==:`
	const clientCertificate = `:MIIBqDCCAU6gAwIBAgIBBzAKBggqhkjOPQQDAjA6MRswGQYDVQQKDBJMZXQncyBBdXRoZW50aWNhdGUxGzAZBgNVBAMMEkxBIEludGVybWVkaWF0ZSBDQTAeFw0yMDAxMTQyMjU1MzNaFw0yMTAxMjMyMjU1MzNaMA0xCzAJBgNVBAMMAkJDMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE8YnXXfaUgmnMtOXU/IncWalRhebrXmckC8vdgJ1p5Be5F/3YC8OthxM4+k1M6aEAEFcGzkJiNy6J84y7uzo9M6NyMHAwCQYDVR0TBAIwADAfBgNVHSMEGDAWgBRm3WjLa38lbEYCuiCPct0ZaSED2DAOBgNVHQ8BAf8EBAMCBsAwEwYDVR0lBAwwCgYIKwYBBQUHAwIwHQYDVR0RAQH/BBMwEYEPYmRjQGV4YW1wbGUuY29tMAoGCCqGSM49BAMCA0gAMEUCIBHda/r1vaL6G3VliL4/Di6YK0Q6bMjeSkC3dFCOOB8TAiEAx/kHSB4urmiZ0NX5r5XarmPk0wmuydBVoU4hBVZ1yhk=:`
	const wantBase = `"@path": /foo
"@query": ?param=Value&Pet=dog
"@method": POST
"@authority": service.internal.example
"client-cert": ` + clientCertificate + `
"@signature-params": ("@path" "@query" "@method" "@authority" "client-cert");created=1618884473;keyid="test-key-ecc-p256"`

	request, err := http.NewRequest(http.MethodPost, "https://service.internal.example/foo?param=Value&Pet=dog", strings.NewReader(`{"hello": "world"}`))
	if err != nil {
		t.Fatalf("NewRequest(RFC 9421 Appendix B.3) error = %v", err)
	}
	request.Header.Set("Client-Cert", clientCertificate)

	inputs, err := ParseSignatureInputs([]string{inputField})
	if err != nil {
		t.Fatalf("ParseSignatureInputs(RFC 9421 Appendix B.3) error = %v", err)
	}
	if got := inputs.String(); got != inputField {
		t.Fatalf("Signature-Input canonical form = %q, want %q", got, inputField)
	}
	signatures, err := ParseSignatures([]string{signatureField})
	if err != nil {
		t.Fatalf("ParseSignatures(RFC 9421 Appendix B.3) error = %v", err)
	}
	if got := signatures.String(); got != signatureField {
		t.Fatalf("Signature canonical form = %q, want %q", got, signatureField)
	}

	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatalf("CreateSignatureBase(RFC 9421 Appendix B.3) error = %v", err)
	}
	if base != wantBase {
		t.Fatalf("signature base =\n%s\nwant =\n%s", base, wantBase)
	}
	if err := Verify(context.Background(), ECDSAP256SHA256, rfc9421P256PublicKey(t), []byte(base), signatures.Entries()[0].Value); err != nil {
		t.Fatalf("Verify(RFC 9421 Appendix B.3) error = %v", err)
	}
}

func TestRFC9421AppendixB4HTTPMessageTransformations(t *testing.T) {
	t.Parallel()

	const inputField = `transform=("@method" "@path" "@authority" "accept");created=1618884473;keyid="test-key-ed25519"`
	const signatureField = `transform=:ZT1kooQsEHpZ0I1IjCqtQppOmIqlJPeo7DHR3SoMn0s5JZ1eRGS0A+vyYP9t/LXlh5QMFFQ6cpLt2m0pmj3NDA==:`
	const wantBase = `"@method": GET
"@path": /demo
"@authority": example.org
"accept": application/json, */*
"@signature-params": ("@method" "@path" "@authority" "accept");created=1618884473;keyid="test-key-ed25519"`

	inputs, err := ParseSignatureInputs([]string{inputField})
	if err != nil {
		t.Fatalf("ParseSignatureInputs(RFC 9421 Appendix B.4) error = %v", err)
	}
	if got := inputs.String(); got != inputField {
		t.Fatalf("Signature-Input canonical form = %q, want %q", got, inputField)
	}
	signatures, err := ParseSignatures([]string{signatureField})
	if err != nil {
		t.Fatalf("ParseSignatures(RFC 9421 Appendix B.4) error = %v", err)
	}
	if got := signatures.String(); got != signatureField {
		t.Fatalf("Signature canonical form = %q, want %q", got, signatureField)
	}
	input, signature := inputs.Entries()[0], signatures.Entries()[0].Value
	publicKey := ed25519.PublicKey(decodeRawURL(t, "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs"))

	tests := []struct {
		name      string
		method    string
		target    string
		wire      string
		accept    []string
		headers   http.Header
		wantValid bool
	}{
		{
			name:      "original signed message",
			method:    http.MethodGet,
			target:    "https://example.org/demo?name1=Value1&Name2=value2",
			accept:    []string{"application/json", "*/*"},
			headers:   http.Header{"Date": []string{"Fri, 15 Jul 2022 14:24:55 GMT"}},
			wantValid: true,
		},
		{
			name:      "uncovered query and accept-language added",
			method:    http.MethodGet,
			target:    "https://example.org/demo?name1=Value1&Name2=value2&param=added",
			accept:    []string{"application/json", "*/*"},
			headers:   http.Header{"Date": []string{"Fri, 15 Jul 2022 14:24:55 GMT"}, "Accept-Language": []string{"en-US,en;q=0.5"}},
			wantValid: true,
		},
		{
			name:      "uncovered fields changed and accept combined",
			method:    http.MethodGet,
			target:    "https://example.org/demo?name1=Value1&Name2=value2",
			accept:    []string{"application/json, */*"},
			headers:   http.Header{"Referer": []string{"https://developer.example.org/demo"}},
			wantValid: true,
		},
		{
			name: "unrelated field lines reordered",
			wire: "GET /demo?name1=Value1&Name2=value2 HTTP/1.1\r\n" +
				"Accept: application/json\r\n" +
				"Accept: */*\r\n" +
				"Date: Fri, 15 Jul 2022 14:24:55 GMT\r\n" +
				"Host: example.org\r\n" +
				"Signature-Input: " + inputField + "\r\n" +
				"Signature: " + signatureField + "\r\n\r\n",
			wantValid: true,
		},
		{
			name:      "covered method and authority changed",
			method:    http.MethodPost,
			target:    "https://example.com/demo?name1=Value1&Name2=value2",
			accept:    []string{"application/json", "*/*"},
			headers:   http.Header{"Date": []string{"Fri, 15 Jul 2022 14:24:55 GMT"}},
			wantValid: false,
		},
		{
			name:      "covered accept field values reordered",
			method:    http.MethodGet,
			target:    "https://example.org/demo?name1=Value1&Name2=value2",
			accept:    []string{"*/*", "application/json"},
			headers:   http.Header{"Date": []string{"Fri, 15 Jul 2022 14:24:55 GMT"}},
			wantValid: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var request *http.Request
			var err error
			if test.wire != "" {
				request, err = http.ReadRequest(bufio.NewReader(strings.NewReader(test.wire)))
			} else {
				request, err = http.NewRequest(test.method, test.target, nil)
			}
			if err != nil {
				t.Fatalf("parse request (RFC 9421 Appendix B.4) error = %v", err)
			}
			if test.wire == "" {
				request.Header = test.headers.Clone()
				for _, value := range test.accept {
					request.Header.Add("Accept", value)
				}
			}
			base, err := CreateSignatureBase(MessageContext{Request: request}, input)
			if err != nil {
				t.Fatalf("CreateSignatureBase(RFC 9421 Appendix B.4) error = %v", err)
			}
			verifyErr := Verify(context.Background(), Ed25519, publicKey, []byte(base), signature)
			if test.wantValid {
				if base != wantBase {
					t.Fatalf("signature base =\n%s\nwant =\n%s", base, wantBase)
				}
				if verifyErr != nil {
					t.Fatalf("Verify(RFC 9421 Appendix B.4 valid transformation) error = %v", verifyErr)
				}
				return
			}
			if base == wantBase {
				t.Fatalf("unsafe transformation retained signature base =\n%s", base)
			}
			if !errors.Is(verifyErr, ErrInvalidSignatureValue) {
				t.Fatalf("Verify(RFC 9421 Appendix B.4 invalid transformation) error = %v, want ErrInvalidSignatureValue", verifyErr)
			}
		})
	}
}

func TestRFC9530AppendixBExamples(t *testing.T) {
	t.Parallel()

	const (
		fullRepresentation = "{\"hello\": \"world\"}\n"
		fullSHA256         = "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:"
		emptySHA256        = "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:"
		partialSHA256      = "sha-256=:jjcgBDWNAtbYUXI37CVG3gRuGOAjaaDRGpIUFsdyepQ=:"
		compressedSHA256   = "sha-256=:d435Qo+nKZ+gLcUHn7GQtQ72hiBVAgqoLsZnZPiTGPk=:"
		compressedDigests  = "sha-256=:d435Qo+nKZ+gLcUHn7GQtQ72hiBVAgqoLsZnZPiTGPk=:, sha-512=:db7fdBbgZMgX1Wb2MjA8zZj+rSNgfmDCEEXM8qLWfpfoNY0sCpHAzZbj09X1/7HAb7Od5Qfto4QpuBsFbUO3dQ==:"
		titleSHA256        = "sha-256=:mEkdbO7Srd9LIOegftO0aBX+VPTVz7/CSHes2Z27gc4=:"
		resourceSHA256     = "sha-256=:uVSlinTTdQUwm2On4k8TJUikGN1bf/Ds8WPX4oe0h9I=:"
		statusSHA256       = "sha-256=:yXIGDTN5VrfoyisKlXgRKUHHMs35SNtyC3szSz1dbO8=:"
		errorSHA256        = "sha-256=:EXB0S2VF2H7ijkAVJkH1Sm0pBho0iDZcvVUHHXTTZSA=:"
	)
	correctedCompressed := decodeHex(t, "0b09807b2268656c6c6f223a2022776f726c64227d0a03")

	t.Run("B.1 server returns full representation data", func(t *testing.T) {
		t.Parallel()

		contentDigest := assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		representationDigest := assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		if contentDigest.String() != representationDigest.String() {
			t.Fatalf("Content-Digest = %q, Repr-Digest = %q", contentDigest.String(), representationDigest.String())
		}
	})

	t.Run("B.2 server returns no representation data", func(t *testing.T) {
		t.Parallel()

		contentDigest := assertRFC9530Digest(t, nil, []DigestAlgorithm{SHA256}, emptySHA256)
		representationDigest := assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		if contentDigest.String() == representationDigest.String() {
			t.Fatalf("empty Content-Digest unexpectedly equals Repr-Digest %q", contentDigest.String())
		}
	})

	t.Run("B.3 server returns partial representation data", func(t *testing.T) {
		t.Parallel()

		contentDigest := assertRFC9530Digest(t, []byte("\"world\"}\n"), []DigestAlgorithm{SHA256}, partialSHA256)
		representationDigest := assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		if contentDigest.String() == representationDigest.String() {
			t.Fatalf("partial Content-Digest unexpectedly equals full Repr-Digest %q", contentDigest.String())
		}
	})

	t.Run("B.4 client and server provide full representation data", func(t *testing.T) {
		t.Parallel()

		assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		assertRFC9530Digest(t, correctedCompressed, []DigestAlgorithm{SHA256}, compressedSHA256)

		// Reported erratum 8890 changes the first two published payload bytes.
		publishedBytes := decodeHex(t, "8b08807b2268656c6c6f223a2022776f726c64227d0a03")
		publishedDigest, err := ComputeDigests([]DigestAlgorithm{SHA256}, publishedBytes)
		if err != nil {
			t.Fatalf("ComputeDigests(RFC 9530 Appendix B.4 published bytes) error = %v", err)
		}
		const wantPublishedBytesDigest = "sha-256=:MklYnI/SsUF/5X7enJ2TU+DFjodRObdKLFaPPLe/Kcw=:"
		if got := publishedDigest.String(); got != wantPublishedBytesDigest || got == compressedSHA256 {
			t.Fatalf("published-byte digest = %q, want %q and distinct from corrected %q", got, wantPublishedBytesDigest, compressedSHA256)
		}
	})

	t.Run("B.5 client provides full data and server provides no data", func(t *testing.T) {
		t.Parallel()

		assertRFC9530PublishedExtraPaddingRejected(t, "RFC 9530 Appendix B.5", "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg==:")
		assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		assertRFC9530Digest(t, correctedCompressed, []DigestAlgorithm{SHA256}, compressedSHA256)
	})

	t.Run("B.6 response provides two representation digests", func(t *testing.T) {
		t.Parallel()

		assertRFC9530PublishedExtraPaddingRejected(t, "RFC 9530 Appendix B.6", "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg==:")
		assertRFC9530Digest(t, []byte(fullRepresentation), []DigestAlgorithm{SHA256}, fullSHA256)
		assertRFC9530Digest(t, correctedCompressed, []DigestAlgorithm{SHA256, SHA512}, compressedDigests)
	})

	t.Run("B.7 POST response references Content-Location", func(t *testing.T) {
		t.Parallel()

		assertRFC9530Digest(t, []byte("{\"title\": \"New Title\"}\n"), []DigestAlgorithm{SHA256}, titleSHA256)
		resource := []byte("{\n  \"id\": \"123\",\n  \"title\": \"New Title\"\n}\n")
		assertRFC9530Digest(t, resource, []DigestAlgorithm{SHA256}, resourceSHA256)
	})

	t.Run("B.8 POST response describes request status", func(t *testing.T) {
		t.Parallel()

		assertRFC9530Digest(t, []byte("{\"title\": \"New Title\"}\n"), []DigestAlgorithm{SHA256}, titleSHA256)
		status := []byte("{\n  \"status\": \"created\",\n  \"id\": \"123\",\n  \"ts\": 1569327729,\n  \"instance\": \"/books/123\"\n}\n")
		assertRFC9530Digest(t, status, []DigestAlgorithm{SHA256}, statusSHA256)
	})

	t.Run("B.9 digest with PATCH", func(t *testing.T) {
		t.Parallel()

		assertRFC9530Digest(t, []byte("{\"title\": \"New Title\"}\n"), []DigestAlgorithm{SHA256}, titleSHA256)
		resource := []byte("{\n  \"id\": \"123\",\n  \"title\": \"New Title\"\n}\n")
		assertRFC9530Digest(t, resource, []DigestAlgorithm{SHA256}, resourceSHA256)
		assertRFC9530Digest(t, nil, []DigestAlgorithm{SHA256}, emptySHA256)
	})

	t.Run("B.10 error response representation", func(t *testing.T) {
		t.Parallel()

		problem := []byte("{\n  \"title\": \"Not Found\",\n  \"detail\": \"Cannot PATCH a non-existent resource\",\n  \"status\": 404\n}\n")
		assertRFC9530Digest(t, problem, []DigestAlgorithm{SHA256}, errorSHA256)
	})

	t.Run("B.11 trailer field and transfer coding", func(t *testing.T) {
		t.Parallel()

		const publishedTrailer = "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg==:"
		wire := "HTTP/1.1 200 OK\r\n" +
			"Content-Type: application/json\r\n" +
			"Transfer-Encoding: chunked\r\n" +
			"Trailer: Repr-Digest\r\n\r\n" +
			"8\r\n{\"hello\"\r\n" +
			"8\r\n: \"world\r\n" +
			"3\r\n\"}\n\r\n" +
			"0\r\nRepr-Digest: " + publishedTrailer + "\r\n\r\n"
		request, err := http.NewRequest(http.MethodGet, "http://foo.example/items/123", nil)
		if err != nil {
			t.Fatalf("NewRequest(RFC 9530 Appendix B.11) error = %v", err)
		}
		response, err := http.ReadResponse(bufio.NewReader(strings.NewReader(wire)), request)
		if err != nil {
			t.Fatalf("ReadResponse(RFC 9530 Appendix B.11) error = %v", err)
		}
		content, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("ReadAll(RFC 9530 Appendix B.11) error = %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("Close(RFC 9530 Appendix B.11) error = %v", err)
		}
		if got := string(content); got != fullRepresentation {
			t.Fatalf("decoded chunked content = %q, want %q", got, fullRepresentation)
		}
		if got := response.Trailer.Get("Repr-Digest"); got != publishedTrailer {
			t.Fatalf("Repr-Digest trailer = %q, want %q", got, publishedTrailer)
		}
		assertRFC9530PublishedExtraPaddingRejected(t, "RFC 9530 Appendix B.11", publishedTrailer)
		assertRFC9530Digest(t, content, []DigestAlgorithm{SHA256}, fullSHA256)
	})
}

func assertRFC9530Digest(t *testing.T, content []byte, algorithms []DigestAlgorithm, want string) DigestField {
	t.Helper()

	computed, err := ComputeDigests(algorithms, content)
	if err != nil {
		t.Fatalf("ComputeDigests(RFC 9530 example) error = %v", err)
	}
	if got := computed.String(); got != want {
		t.Fatalf("computed digest = %q, want %q", got, want)
	}
	parsed, err := ParseDigestField(want)
	if err != nil {
		t.Fatalf("ParseDigestField(RFC 9530 example) error = %v", err)
	}
	if got := parsed.String(); got != want {
		t.Fatalf("canonical digest = %q, want %q", got, want)
	}
	if err := parsed.Verify(content, algorithms); err != nil {
		t.Fatalf("DigestField.Verify(RFC 9530 example) error = %v", err)
	}
	return parsed
}

func assertRFC9530PublishedExtraPaddingRejected(t *testing.T, section, field string) {
	t.Helper()
	// B.5, B.6, and B.11 print a 45-character base64 value ending in "==".
	// The 32-byte SHA-256 value has a 44-character canonical encoding ending in
	// one "=", so accepting the published form would create a parser differential.
	if _, err := ParseDigestField(field); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("ParseDigestField(%s published extra padding) error = %v, want ErrInvalidDigestField", section, err)
	}
}

func rfc9421TestRequest() MessageContext {
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/foo?param=Value&Pet=dog", strings.NewReader(`{"hello": "world"}`))
	request.Header.Set("Date", "Tue, 20 Apr 2021 02:07:55 GMT")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Digest", "sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:")
	request.Header.Set("Content-Length", "18")
	return MessageContext{Request: request}
}

func rfc9421TestResponse() MessageContext {
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: 23}
	response.Header.Set("Date", "Tue, 20 Apr 2021 02:07:56 GMT")
	response.Header.Set("Content-Type", "application/json")
	response.Header.Set("Content-Digest", "sha-512=:mEWXIS7MaLRuGgxOBdODa3xqM1XdEvxoYhvlCFJ41QJgJc4GTsPp29l5oGX69wWdXymyU0rjJuahq4l5aGgfLQ==:")
	response.Header.Set("Content-Length", "23")
	return MessageContext{Response: response, ResponseTransport: ResponseTransportReceived}
}
