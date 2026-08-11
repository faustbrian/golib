package interoperability_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	httpsignature "github.com/faustbrian/golib/pkg/http-signature"
)

const differentialKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // gitleaks:allow -- Public fixture key.

type corpus struct {
	StructuredFields []structuredFieldCase `json:"structured_fields"`
	SignatureBases   []signatureBaseCase   `json:"signature_bases"`
}

type structuredFieldCase struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Fields        []string `json:"fields"`
	Valid         bool     `json:"valid"`
	Canonical     string   `json:"canonical"`
	PeerValid     *bool    `json:"peer_valid"`
	PeerCanonical string   `json:"peer_canonical"`
	Divergence    string   `json:"divergence"`
}

type signatureBaseCase struct {
	Name             string              `json:"name"`
	Method           string              `json:"method"`
	Target           string              `json:"target"`
	Body             string              `json:"body"`
	Headers          map[string][]string `json:"headers"`
	Components       []string            `json:"components"`
	SignatureInput   string              `json:"signature_input"`
	StructuredFields map[string]string   `json:"structured_fields"`
	ExpectedBase     string              `json:"expected_base"`
}

type peerSignature struct {
	Peer      string
	Input     string
	Signature string
}

func TestStructuredFieldsSharedCorpus(t *testing.T) {
	t.Parallel()

	for _, test := range loadCorpus(t).StructuredFields {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			local, localErr := canonicalizeLocal(test.Kind, test.Fields)
			peer, peerErr := canonicalizePeerSF(test.Fields)
			peerValid := test.Valid
			if test.PeerValid != nil {
				peerValid = *test.PeerValid
			}
			if (localErr == nil) != test.Valid {
				t.Fatalf("local validity = %t, want %t (error = %v)", localErr == nil, test.Valid, localErr)
			}
			if (peerErr == nil) != peerValid {
				t.Fatalf("peer validity = %t, want %t (error = %v)", peerErr == nil, peerValid, peerErr)
			}
			if test.Valid && local != test.Canonical {
				t.Fatalf("local canonical form = %q, want %q", local, test.Canonical)
			}
			peerCanonical := test.PeerCanonical
			if peerCanonical == "" {
				peerCanonical = test.Canonical
			}
			if peerValid && peer != peerCanonical {
				t.Fatalf("peer canonical form = %q, want %q", peer, peerCanonical)
			}

			diverges := test.Valid != peerValid || test.Valid && local != peer
			if diverges && test.Divergence == "" {
				t.Fatal("an observed parser divergence must carry an explicit corpus decision")
			}
			if !diverges && test.Divergence != "" {
				t.Fatalf("declared divergence %q was not observed", test.Divergence)
			}
		})
	}
}

func TestSignatureBasesSharedCorpus(t *testing.T) {
	t.Parallel()

	for _, test := range loadCorpus(t).SignatureBases {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			base, err := createLocalBase(newRequest(t, test), test.SignatureInput, test.StructuredFields)
			if err != nil {
				t.Fatalf("local signature base: %v", err)
			}
			if base != test.ExpectedBase {
				t.Fatalf("local base = %q, want %q", base, test.ExpectedBase)
			}

			peers := map[string]func(*http.Request, signatureBaseCase) (peerSignature, error){
				"yaronf/httpsign": signYaronF,
				"dadrus/httpsig":  signDadrus,
			}
			for name, sign := range peers {
				t.Run(name, func(t *testing.T) {
					result, signErr := sign(newRequest(t, test), test)
					if signErr != nil {
						t.Fatalf("peer sign: %v", signErr)
					}
					assertPeerBaseEquivalent(t, test, result)
				})
			}
		})
	}
}

func assertPeerBaseEquivalent(t *testing.T, test signatureBaseCase, result peerSignature) {
	t.Helper()

	expectedInput, err := parseSignatureInput(test.SignatureInput)
	if err != nil {
		t.Fatalf("parse expected Signature-Input: %v", err)
	}
	peerInputValue, err := parseSignatureInput(result.Input)
	if err != nil {
		t.Fatalf("parse peer Signature-Input: %v", err)
	}
	if !reflect.DeepEqual(peerInputValue.Components, expectedInput.Components) {
		t.Fatalf("peer covered components = %#v, want %#v", peerInputValue.Components, expectedInput.Components)
	}
	assertPeerSignatureParameters(t, result.Peer, peerInputValue.Parameters)

	base, err := createLocalBase(newRequest(t, test), result.Input, test.StructuredFields)
	if err != nil {
		t.Fatalf("reconstruct peer base: %v", err)
	}
	if componentLines(base) != componentLines(test.ExpectedBase) {
		t.Fatalf("peer component base = %q, want %q", componentLines(base), componentLines(test.ExpectedBase))
	}

	localInput, err := canonicalizeLocal("signature-input", []string{result.Input})
	if err != nil {
		t.Fatalf("local peer Signature-Input parse: %v", err)
	}
	peerInput, err := canonicalizePeerSF([]string{result.Input})
	if err != nil {
		t.Fatalf("independent peer Signature-Input parse: %v", err)
	}
	if localInput != peerInput {
		t.Fatalf("peer Signature-Input canonical forms differ: local = %q, SF peer = %q", localInput, peerInput)
	}

	signature, err := signatureBytes(result.Signature)
	if err != nil {
		t.Fatalf("peer Signature parse: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(differentialKey))
	_, _ = mac.Write([]byte(base))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		t.Fatal("peer signature does not authenticate the locally reconstructed byte-for-byte base")
	}
}

func assertPeerSignatureParameters(t *testing.T, peer string, parameters []httpsignature.Parameter) {
	t.Helper()

	names := make([]string, len(parameters))
	for index, parameter := range parameters {
		names[index] = parameter.Name
		switch parameter.Name {
		case "created":
			created, ok := parameter.Value.(int64)
			if !ok || created <= 0 {
				t.Fatalf("peer created parameter = %#v, want a positive integer", parameter.Value)
			}
		case "keyid":
			if parameter.Value != "differential-key" {
				t.Fatalf("peer keyid parameter = %#v", parameter.Value)
			}
		case "alg":
			if parameter.Value != "hmac-sha256" {
				t.Fatalf("peer alg parameter = %#v", parameter.Value)
			}
		case "nonce":
			if parameter.Value != "differential-nonce" {
				t.Fatalf("peer nonce parameter = %#v", parameter.Value)
			}
		default:
			t.Fatalf("peer emitted unexpected signature parameter %q", parameter.Name)
		}
	}

	var want []string
	switch peer {
	case "yaronf/httpsign":
		want = []string{"alg", "keyid"}
	case "dadrus/httpsig":
		want = []string{"created", "keyid", "alg", "nonce"}
	default:
		t.Fatalf("unknown peer %q", peer)
	}
	if !slices.Equal(names, want) {
		t.Fatalf("peer signature parameter order = %v, want %v", names, want)
	}
}

func newRequest(t *testing.T, test signatureBaseCase) *http.Request {
	t.Helper()

	request, err := http.NewRequest(test.Method, test.Target, strings.NewReader(test.Body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Host = request.URL.Host
	for name, values := range test.Headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	return request
}

func componentLines(base string) string {
	index := strings.LastIndex(base, "\n\"@signature-params\": ")
	if index == -1 {
		return ""
	}
	return base[:index]
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()

	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var value corpus
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode corpus: %v", err)
	}
	if len(value.StructuredFields) == 0 || len(value.SignatureBases) == 0 {
		t.Fatal("corpus must contain Structured Fields and signature-base cases")
	}

	return value
}
