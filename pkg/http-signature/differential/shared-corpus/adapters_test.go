package interoperability_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	peerDadrus "github.com/dadrus/httpsig"
	httpsignature "github.com/faustbrian/golib/pkg/http-signature"
	peersfv "github.com/shogo82148/go-sfv"
	peerYaronF "github.com/yaronf/httpsign"
)

func canonicalizeLocal(kind string, fields []string) (string, error) {
	switch kind {
	case "signature-input":
		value, err := httpsignature.ParseSignatureInputs(fields)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case "signature":
		value, err := httpsignature.ParseSignatures(fields)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case "accept-signature":
		value, err := httpsignature.ParseAcceptSignatures(fields)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case "content-digest":
		value, err := httpsignature.ParseDigestFields(fields)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case "digest-preference":
		value, err := httpsignature.ParseDigestPreferences(fields)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	default:
		return "", fmt.Errorf("unknown local Structured Field kind %q", kind)
	}
}

func canonicalizePeerSF(fields []string) (string, error) {
	dictionary, err := peersfv.DecodeDictionary(fields)
	if err != nil {
		return "", err
	}
	return peersfv.EncodeDictionary(dictionary)
}

func createLocalBase(request *http.Request, inputField string, structuredFields map[string]string) (string, error) {
	input, err := parseSignatureInput(inputField)
	if err != nil {
		return "", err
	}
	types := make(map[string]httpsignature.StructuredFieldType, len(structuredFields))
	for name, fieldType := range structuredFields {
		switch fieldType {
		case "dictionary":
			types[name] = httpsignature.StructuredFieldDictionary
		case "list":
			types[name] = httpsignature.StructuredFieldList
		case "item":
			types[name] = httpsignature.StructuredFieldItem
		default:
			return "", fmt.Errorf("unknown Structured Field type %q for %q", fieldType, name)
		}
	}
	return httpsignature.CreateSignatureBase(httpsignature.MessageContext{
		Request:          request,
		StructuredFields: types,
	}, input)
}

func parseSignatureInput(inputField string) (httpsignature.SignatureInput, error) {
	inputs, err := httpsignature.ParseSignatureInputs([]string{inputField})
	if err != nil {
		return httpsignature.SignatureInput{}, err
	}
	return signatureInputByLabel(inputs, "sig")
}

func signatureInputByLabel(inputs httpsignature.SignatureInputs, label string) (httpsignature.SignatureInput, error) {
	for _, input := range inputs.Entries() {
		if input.Label == label {
			return input, nil
		}
	}
	return httpsignature.SignatureInput{}, fmt.Errorf("Signature-Input label %q is absent", label)
}

func signatureBytes(field string) ([]byte, error) {
	signatures, err := httpsignature.ParseSignatures([]string{field})
	if err != nil {
		return nil, err
	}
	for _, signature := range signatures.Entries() {
		if signature.Label == "sig" {
			return signature.Value, nil
		}
	}
	return nil, fmt.Errorf("Signature label %q is absent", "sig")
}

func signYaronF(request *http.Request, test signatureBaseCase) (peerSignature, error) {
	fields, err := yaronFFields(test.Components)
	if err != nil {
		return peerSignature{}, err
	}
	config := peerYaronF.NewSignConfig().
		SignCreated(false).
		SetKeyID("differential-key").
		SetSchemeFromRequest(func(request *http.Request) string { return request.URL.Scheme })
	signer, err := peerYaronF.NewHMACSHA256Signer([]byte(differentialKey), config, *fields)
	if err != nil {
		return peerSignature{}, err
	}
	input, signature, err := peerYaronF.SignRequest("sig", *signer, request)
	return peerSignature{Peer: "yaronf/httpsign", Input: input, Signature: signature}, err
}

func yaronFFields(components []string) (*peerYaronF.Fields, error) {
	fields := peerYaronF.NewFields()
	for _, component := range components {
		switch {
		case strings.HasPrefix(component, `@query-param;name="`) && strings.HasSuffix(component, `"`):
			name := strings.TrimSuffix(strings.TrimPrefix(component, `@query-param;name="`), `"`)
			fields.AddQueryParam(name)
		case strings.HasSuffix(component, ";sf"):
			fields.AddStructuredField(strings.TrimSuffix(component, ";sf"))
		case !strings.Contains(component, ";"):
			fields.AddHeader(component)
		default:
			return nil, fmt.Errorf("component %q is outside the yaronf shared adapter subset", component)
		}
	}
	return fields, nil
}

func signDadrus(request *http.Request, test signatureBaseCase) (peerSignature, error) {
	signer, err := peerDadrus.NewSigner(peerDadrus.Key{
		KeyID:     "differential-key",
		Algorithm: peerDadrus.HmacSha256,
		Key:       []byte(differentialKey),
	}, peerDadrus.WithLabel("sig"), peerDadrus.WithTTL(0), peerDadrus.WithComponents(test.Components...),
		peerDadrus.WithContentDigestAlgorithm(peerDadrus.Sha512),
		peerDadrus.WithNonce(peerDadrus.NonceGetterFunc(func(context.Context) (string, error) {
			return "differential-nonce", nil
		})))
	if err != nil {
		return peerSignature{}, err
	}
	header, err := signer.Sign(peerDadrus.MessageFromRequest(request))
	if err != nil {
		return peerSignature{}, err
	}
	return peerSignature{
		Peer:      "dadrus/httpsig",
		Input:     header.Get("Signature-Input"),
		Signature: header.Get("Signature"),
	}, nil
}
