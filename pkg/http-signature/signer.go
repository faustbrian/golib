package httpsignature

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	// ErrInvalidSigningProfile reports an incomplete or contradictory signing
	// policy.
	ErrInvalidSigningProfile = errors.New("http signature: invalid signing profile")
	// ErrSigningPolicy reports that a signing request violates its profile.
	ErrSigningPolicy = errors.New("http signature: signing policy rejected request")
	// ErrSigningKey reports unavailable, expired, revoked, mismatched, or
	// incompatible signing material.
	ErrSigningKey = errors.New("http signature: signing key unavailable")
	// ErrSigningProvider reports a provider failure without retaining backend
	// details in the public error chain.
	ErrSigningProvider = errors.New("http signature: signing key provider failed")
	// ErrSigningBase reports signature-base construction failure.
	ErrSigningBase = errors.New("http signature: signing base unavailable")
	// ErrSigningCryptographic reports failure of the selected signing primitive.
	ErrSigningCryptographic = errors.New("http signature: cryptographic signing failed")
	// ErrInvalidSignedFields reports an empty, malformed, mismatched, or
	// duplicate set supplied for deterministic field combination.
	ErrInvalidSignedFields = errors.New("http signature: invalid signed fields")
)

// SigningKey binds private or shared signing material to an identifier,
// algorithm, validity interval, and revocation decision.
type SigningKey struct {
	KeyID     string
	Algorithm Algorithm
	Key       any
	NotBefore time.Time
	NotAfter  time.Time
	Revoked   bool
}

// SigningKeyProvider selects current signing material under a bounded context.
// Implementations own key storage and rotation; the core performs no IO.
type SigningKeyProvider interface {
	SigningKey(context.Context) (SigningKey, error)
}

// SigningProfileConfig defines all signing decisions. Creation time and keyid
// are always included as required by this signing API. Other registered
// parameters must be explicitly required or forbidden.
type SigningProfileConfig struct {
	AllowedAlgorithms  []Algorithm
	CoveredComponents  []ComponentIdentifier
	AllowEmptyCoverage bool
	Expires            ParameterPolicy
	AlgorithmParameter ParameterPolicy
	Nonce              ParameterPolicy
	Tag                ParameterPolicy
	TagValue           string
	Lifetime           time.Duration
	ResolveTimeout     time.Duration
	Now                func() time.Time
	Provider           SigningKeyProvider
	// Random is retained for source compatibility.
	//
	// Deprecated: Random is ignored. Randomized algorithms use Go-managed
	// cryptographically secure randomness.
	Random                        io.Reader
	RequireExternalRequestContext bool
}

// SigningProfile is an immutable application signing policy.
type SigningProfile struct {
	algorithms             map[Algorithm]struct{}
	components             []ComponentIdentifier
	allowEmptyCoverage     bool
	expires                ParameterPolicy
	algorithmParameter     ParameterPolicy
	nonce                  ParameterPolicy
	tag                    ParameterPolicy
	tagValue               string
	lifetime               time.Duration
	resolveTimeout         time.Duration
	now                    func() time.Time
	provider               SigningKeyProvider
	requireExternalRequest bool
}

// NewSigningProfile validates and copies an explicit signing policy.
func NewSigningProfile(config SigningProfileConfig) (*SigningProfile, error) {
	if len(config.AllowedAlgorithms) == 0 || config.Now == nil || config.Provider == nil || config.ResolveTimeout <= 0 ||
		!signingParameterPolicy(config.Expires) || !signingParameterPolicy(config.AlgorithmParameter) ||
		!signingParameterPolicy(config.Nonce) || !signingParameterPolicy(config.Tag) {
		return nil, ErrInvalidSigningProfile
	}
	if len(config.CoveredComponents) == 0 && !config.AllowEmptyCoverage {
		return nil, ErrInvalidSigningProfile
	}
	if config.Expires == ParameterRequired && (config.Lifetime < time.Second || config.Lifetime%time.Second != 0) ||
		config.Expires == ParameterForbidden && config.Lifetime != 0 {
		return nil, ErrInvalidSigningProfile
	}
	if config.Tag == ParameterRequired && (config.TagValue == "" || !validSFStringValue(config.TagValue)) ||
		config.Tag == ParameterForbidden && config.TagValue != "" {
		return nil, ErrInvalidSigningProfile
	}

	algorithms := make(map[Algorithm]struct{}, len(config.AllowedAlgorithms))
	for _, algorithm := range config.AllowedAlgorithms {
		if !supportedAlgorithm(algorithm) {
			return nil, ErrInvalidSigningProfile
		}
		if _, duplicate := algorithms[algorithm]; duplicate {
			return nil, ErrInvalidSigningProfile
		}
		algorithms[algorithm] = struct{}{}
	}

	components := make([]ComponentIdentifier, len(config.CoveredComponents))
	seen := make(map[string]struct{}, len(components))
	for index, component := range config.CoveredComponents {
		identifier, err := componentComparisonKey(component)
		if err != nil || !validProfileComponent(component) {
			return nil, ErrInvalidSigningProfile
		}
		if _, duplicate := seen[identifier]; duplicate {
			return nil, ErrInvalidSigningProfile
		}
		seen[identifier] = struct{}{}
		components[index] = ComponentIdentifier{Name: component.Name, Parameters: cloneParameters(component.Parameters)}
	}

	return &SigningProfile{
		algorithms:             algorithms,
		components:             components,
		allowEmptyCoverage:     config.AllowEmptyCoverage,
		expires:                config.Expires,
		algorithmParameter:     config.AlgorithmParameter,
		nonce:                  config.Nonce,
		tag:                    config.Tag,
		tagValue:               config.TagValue,
		lifetime:               config.Lifetime,
		resolveTimeout:         config.ResolveTimeout,
		now:                    config.Now,
		provider:               config.Provider,
		requireExternalRequest: config.RequireExternalRequestContext,
	}, nil
}

// SigningOptions supplies per-message values that cannot be safely defaulted.
type SigningOptions struct {
	Nonce string
}

// SignedFields owns one matching Signature-Input and Signature label pair.
type SignedFields struct {
	input     SignatureInput
	signature SignatureValue
}

// SignatureInputField returns a complete canonical field value for this label.
func (signed SignedFields) SignatureInputField() string {
	if !validSignatureLabel(signed.input.Label) {
		return ""
	}
	return SignatureInputs{entries: []SignatureInput{cloneSignatureInput(signed.input)}}.String()
}

// SignatureField returns a complete canonical field value for this label.
func (signed SignedFields) SignatureField() string {
	if !validSignatureLabel(signed.signature.Label) || len(signed.signature.Value) == 0 {
		return ""
	}
	return Signatures{entries: []SignatureValue{{
		Label: signed.signature.Label,
		Value: append([]byte(nil), signed.signature.Value...),
	}}}.String()
}

// CombineSignedFields combines validated label pairs in caller order. It
// rejects duplicate labels and never uses map iteration to choose wire order.
func CombineSignedFields(fields ...SignedFields) (SignatureInputs, Signatures, error) {
	if len(fields) == 0 {
		return SignatureInputs{}, Signatures{}, ErrInvalidSignedFields
	}
	inputs := make([]SignatureInput, 0, len(fields))
	signatures := make([]SignatureValue, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, signed := range fields {
		if !validSignatureLabel(signed.input.Label) || signed.signature.Label != signed.input.Label || len(signed.signature.Value) == 0 {
			return SignatureInputs{}, Signatures{}, ErrInvalidSignedFields
		}
		if _, duplicate := seen[signed.input.Label]; duplicate {
			return SignatureInputs{}, Signatures{}, ErrInvalidSignedFields
		}
		seen[signed.input.Label] = struct{}{}
		inputs = append(inputs, cloneSignatureInput(signed.input))
		signatures = append(signatures, SignatureValue{
			Label: signed.signature.Label,
			Value: append([]byte(nil), signed.signature.Value...),
		})
	}

	return SignatureInputs{entries: inputs}, Signatures{entries: signatures}, nil
}

// Signer applies one immutable application signing profile.
type Signer struct {
	profile *SigningProfile
}

// NewSigner creates a signer with no hidden defaults or network access.
func NewSigner(profile *SigningProfile) *Signer {
	return &Signer{profile: profile}
}

// Sign creates one matching signature field pair without mutating the HTTP
// message or consuming its body.
func (signer *Signer) Sign(ctx context.Context, message MessageContext, label string, options SigningOptions) (SignedFields, error) {
	if ctx == nil {
		return SignedFields{}, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return SignedFields{}, err
	}
	if signer == nil || signer.profile == nil || !validSignatureLabel(label) {
		return SignedFields{}, ErrSigningPolicy
	}
	if signer.profile.requireExternalRequest && message.ExternalRequest == nil {
		return SignedFields{}, ErrSigningPolicy
	}
	if signer.profile.nonce == ParameterRequired && options.Nonce == "" ||
		signer.profile.nonce == ParameterForbidden && options.Nonce != "" {
		return SignedFields{}, ErrSigningPolicy
	}

	resolveContext, cancel := context.WithTimeout(ctx, signer.profile.resolveTimeout)
	key, providerErr := signer.profile.provider.SigningKey(resolveContext)
	resolveContextErr := resolveContext.Err()
	cancel()
	if resolveContextErr != nil {
		return SignedFields{}, resolveContextErr
	}
	if providerErr != nil {
		return SignedFields{}, ErrSigningProvider
	}

	now := signer.profile.now()
	if key.KeyID == "" || key.Key == nil || key.Revoked || key.NotBefore.IsZero() || key.NotAfter.IsZero() ||
		!key.NotAfter.After(key.NotBefore) || now.Before(key.NotBefore) || !now.Before(key.NotAfter) {
		return SignedFields{}, ErrSigningKey
	}
	if _, allowed := signer.profile.algorithms[key.Algorithm]; !allowed {
		return SignedFields{}, ErrSigningKey
	}
	if signer.profile.expires == ParameterRequired && now.Add(signer.profile.lifetime).After(key.NotAfter) {
		return SignedFields{}, ErrSigningKey
	}

	parameters := []Parameter{{Name: "created", Value: now.Unix()}}
	if signer.profile.expires == ParameterRequired {
		parameters = append(parameters, Parameter{Name: "expires", Value: now.Add(signer.profile.lifetime).Unix()})
	}
	if signer.profile.nonce == ParameterRequired {
		parameters = append(parameters, Parameter{Name: "nonce", Value: options.Nonce})
	}
	parameters = append(parameters, Parameter{Name: "keyid", Value: key.KeyID})
	if signer.profile.algorithmParameter == ParameterRequired {
		parameters = append(parameters, Parameter{Name: "alg", Value: string(key.Algorithm)})
	}
	if signer.profile.tag == ParameterRequired {
		parameters = append(parameters, Parameter{Name: "tag", Value: signer.profile.tagValue})
	}
	input := SignatureInput{Label: label, Components: signer.profile.components, Parameters: parameters}
	base, err := CreateSignatureBase(message, input)
	if err != nil {
		return SignedFields{}, ErrSigningBase
	}
	value, err := Sign(ctx, key.Algorithm, key.Key, []byte(base), nil)
	if mapped := mapSigningError(err); mapped != nil {
		return SignedFields{}, mapped
	}

	return SignedFields{
		input:     cloneSignatureInput(input),
		signature: SignatureValue{Label: label, Value: append([]byte(nil), value...)},
	}, nil
}

func mapSigningError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrIncompatibleKey) {
		return ErrSigningKey
	}
	return ErrSigningCryptographic
}

func signingParameterPolicy(policy ParameterPolicy) bool {
	return policy == ParameterForbidden || policy == ParameterRequired
}

func validSignatureLabel(label string) bool {
	return label != "" && dictionaryMemberKey(label) == label
}

func validProfileComponent(component ComponentIdentifier) bool {
	parameters, err := componentParameterSet(component.Parameters)
	if err != nil {
		return false
	}
	if component.Name[0] != '@' {
		return !parameters.hasName
	}
	if parameters.bs || parameters.sf || parameters.tr || parameters.hasKey {
		return false
	}
	switch component.Name {
	case "@query-param":
		return parameters.hasName
	case "@status":
		return !parameters.hasName && !parameters.req
	case "@method", "@target-uri", "@authority", "@scheme", "@request-target", "@path", "@query":
		return !parameters.hasName
	default:
		return false
	}
}
