package httpsignature

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	// ErrInvalidVerificationProfile reports an incomplete or contradictory
	// application policy.
	ErrInvalidVerificationProfile = errors.New("http signature: invalid verification profile")
	// ErrKeyNotFound allows resolvers to distinguish an unknown identifier from
	// transient backend failure without disclosing the identifier.
	ErrKeyNotFound = errors.New("http signature: verification key not found")
	// ErrKeyResolutionFailure reports a resolver failure whose backend details
	// are deliberately not retained in the public error chain.
	ErrKeyResolutionFailure = errors.New("http signature: verification key resolution failed")
	// ErrReplayBackendFailure reports an unknown replay-backend outcome whose
	// details are deliberately not retained in the public error chain.
	ErrReplayBackendFailure = errors.New("http signature: replay backend failed")
)

// ParameterPolicy specifies whether one registered signature parameter is
// prohibited, accepted, or mandatory. The zero value is invalid so profiles
// cannot accidentally inherit a permissive default.
type ParameterPolicy uint8

const (
	ParameterForbidden ParameterPolicy = iota + 1
	ParameterOptional
	ParameterRequired
)

// ResolvedKey binds verification material to exactly one algorithm and an
// explicit validity and cache-freshness interval. Resolver implementations own
// storage, rotation, revocation lookup, caching, and remote IO.
type ResolvedKey struct {
	Algorithm  Algorithm
	Key        any
	NotBefore  time.Time
	NotAfter   time.Time
	FreshUntil time.Time
	Revoked    bool
}

// KeyResolver resolves an opaque key identifier under the supplied bounded
// context. Implementations must return only context-aware, bounded operations.
type KeyResolver interface {
	Resolve(context.Context, string) (ResolvedKey, error)
}

// VerificationProfileConfig defines the application decisions RFC 9421 leaves
// to a profile. Key identifiers are mandatory in this profile implementation;
// static keys can be exposed by a resolver that recognizes a fixed identifier.
type VerificationProfileConfig struct {
	AllowedAlgorithms             []Algorithm
	RequiredComponents            []ComponentIdentifier
	AllowEmptyCoverage            bool
	Created                       ParameterPolicy
	Expires                       ParameterPolicy
	AlgorithmParameter            ParameterPolicy
	Nonce                         ParameterPolicy
	Tag                           ParameterPolicy
	AllowedTags                   []string
	MaxAge                        time.Duration
	ClockSkew                     time.Duration
	ResolveTimeout                time.Duration
	Now                           func() time.Time
	Resolver                      KeyResolver
	Replay                        ReplayStore
	RequireExternalRequestContext bool
}

// VerificationProfile is an immutable application verification policy.
type VerificationProfile struct {
	algorithms             map[Algorithm]struct{}
	requiredComponents     []ComponentIdentifier
	allowEmptyCoverage     bool
	created                ParameterPolicy
	expires                ParameterPolicy
	algorithmParameter     ParameterPolicy
	nonce                  ParameterPolicy
	tag                    ParameterPolicy
	allowedTags            map[string]struct{}
	maxAge                 time.Duration
	clockSkew              time.Duration
	resolveTimeout         time.Duration
	now                    func() time.Time
	resolver               KeyResolver
	replay                 ReplayStore
	requireExternalRequest bool
}

// NewVerificationProfile validates and copies an explicit application policy.
func NewVerificationProfile(config VerificationProfileConfig) (*VerificationProfile, error) {
	if len(config.AllowedAlgorithms) == 0 || config.Now == nil || config.Resolver == nil ||
		config.ResolveTimeout <= 0 || config.ClockSkew < 0 ||
		config.MaxAge > time.Duration(1<<63-1)-config.ClockSkew ||
		!validParameterPolicy(config.Created) || !validParameterPolicy(config.Expires) ||
		!validParameterPolicy(config.AlgorithmParameter) || !validParameterPolicy(config.Nonce) ||
		!validParameterPolicy(config.Tag) {
		return nil, ErrInvalidVerificationProfile
	}
	if config.Created == ParameterForbidden && config.MaxAge != 0 ||
		config.Created != ParameterForbidden && config.MaxAge <= 0 {
		return nil, ErrInvalidVerificationProfile
	}
	if config.Nonce == ParameterForbidden && config.Replay != nil ||
		config.Nonce != ParameterForbidden && config.Replay == nil {
		return nil, ErrInvalidVerificationProfile
	}
	if config.Tag == ParameterForbidden && len(config.AllowedTags) != 0 ||
		config.Tag != ParameterForbidden && len(config.AllowedTags) == 0 {
		return nil, ErrInvalidVerificationProfile
	}
	if len(config.RequiredComponents) == 0 && !config.AllowEmptyCoverage {
		return nil, ErrInvalidVerificationProfile
	}

	algorithms := make(map[Algorithm]struct{}, len(config.AllowedAlgorithms))
	for _, algorithm := range config.AllowedAlgorithms {
		if !supportedAlgorithm(algorithm) {
			return nil, ErrInvalidVerificationProfile
		}
		if _, duplicate := algorithms[algorithm]; duplicate {
			return nil, ErrInvalidVerificationProfile
		}
		algorithms[algorithm] = struct{}{}
	}

	required := make([]ComponentIdentifier, len(config.RequiredComponents))
	seenComponents := make(map[string]struct{}, len(required))
	for index, component := range config.RequiredComponents {
		identifier, err := componentComparisonKey(component)
		if err != nil || !validProfileComponent(component) {
			return nil, ErrInvalidVerificationProfile
		}
		if _, duplicate := seenComponents[identifier]; duplicate {
			return nil, ErrInvalidVerificationProfile
		}
		seenComponents[identifier] = struct{}{}
		required[index] = ComponentIdentifier{Name: component.Name, Parameters: cloneParameters(component.Parameters)}
	}

	tags := make(map[string]struct{}, len(config.AllowedTags))
	for _, tag := range config.AllowedTags {
		if tag == "" || !validSFStringValue(tag) {
			return nil, ErrInvalidVerificationProfile
		}
		if _, duplicate := tags[tag]; duplicate {
			return nil, ErrInvalidVerificationProfile
		}
		tags[tag] = struct{}{}
	}

	return &VerificationProfile{
		algorithms:             algorithms,
		requiredComponents:     required,
		allowEmptyCoverage:     config.AllowEmptyCoverage,
		created:                config.Created,
		expires:                config.Expires,
		algorithmParameter:     config.AlgorithmParameter,
		nonce:                  config.Nonce,
		tag:                    config.Tag,
		allowedTags:            tags,
		maxAge:                 config.MaxAge,
		clockSkew:              config.ClockSkew,
		resolveTimeout:         config.ResolveTimeout,
		now:                    config.Now,
		resolver:               config.Resolver,
		replay:                 config.Replay,
		requireExternalRequest: config.RequireExternalRequestContext,
	}, nil
}

// VerificationFailure is a stable, application-mappable failure category.
type VerificationFailure string

const (
	VerificationSelection     VerificationFailure = "selection"
	VerificationPolicy        VerificationFailure = "policy"
	VerificationTime          VerificationFailure = "time"
	VerificationKeyResolution VerificationFailure = "key-resolution"
	VerificationKey           VerificationFailure = "key"
	VerificationAlgorithm     VerificationFailure = "algorithm"
	VerificationBase          VerificationFailure = "signature-base"
	VerificationCryptographic VerificationFailure = "cryptographic"
	VerificationReplay        VerificationFailure = "replay"
)

// VerificationError is safe for classification and logging. Error omits key
// identifiers, nonces, signature bases, signature bytes, and message content.
type VerificationError struct {
	Failure VerificationFailure
	cause   error
}

func (err *VerificationError) Error() string {
	return "http signature verification failed: " + string(err.Failure)
}

// Unwrap supports errors.Is and errors.As without including the cause in the
// rendered error string.
func (err *VerificationError) Unwrap() error {
	return err.cause
}

// VerifiedSignature reports the selected label and resolved public metadata.
// A successful result proves cryptographic validity and profile conformance;
// it does not grant authentication or authorization.
type VerifiedSignature struct {
	Label     string
	KeyID     string
	Algorithm Algorithm
	Created   time.Time
	Expires   time.Time
}

// Verifier applies one immutable application profile.
type Verifier struct {
	profile *VerificationProfile
}

// NewVerifier creates a verifier with no hidden defaults or network access.
func NewVerifier(profile *VerificationProfile) *Verifier {
	return &Verifier{profile: profile}
}

// Verify selects one explicit label, enforces application policy, resolves an
// algorithm-bound key, verifies the reconstructed signature base, and only
// then atomically consumes the nonce.
func (verifier *Verifier) Verify(
	ctx context.Context,
	message MessageContext,
	label string,
	inputs SignatureInputs,
	signatures Signatures,
) (VerifiedSignature, error) {
	if ctx == nil {
		return VerifiedSignature{}, verificationError(VerificationSelection, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return VerifiedSignature{}, verificationError(VerificationSelection, err)
	}
	if verifier == nil || verifier.profile == nil || label == "" {
		return VerifiedSignature{}, verificationError(VerificationSelection, ErrInvalidVerificationProfile)
	}
	if !matchingLabelSets(inputs, signatures) {
		return VerifiedSignature{}, verificationError(VerificationSelection, errors.New("field label mismatch"))
	}

	input, signature, ok := selectSignature(label, inputs, signatures)
	if !ok {
		return VerifiedSignature{}, verificationError(VerificationSelection, errors.New("label mismatch"))
	}
	if verifier.profile.requireExternalRequest && message.ExternalRequest == nil {
		return VerifiedSignature{}, verificationError(VerificationPolicy, errors.New("trusted external request context required"))
	}
	metadata, err := verifier.profile.validateInput(input)
	if err != nil {
		return VerifiedSignature{}, err
	}

	resolveContext, cancel := context.WithTimeout(ctx, verifier.profile.resolveTimeout)
	resolved, resolveErr := verifier.profile.resolver.Resolve(resolveContext, metadata.keyID)
	resolveContextErr := resolveContext.Err()
	cancel()
	if resolveContextErr != nil {
		return VerifiedSignature{}, verificationError(VerificationKeyResolution, safeResolutionCause(resolveContextErr))
	}
	if resolveErr != nil {
		return VerifiedSignature{}, verificationError(VerificationKeyResolution, safeResolutionCause(resolveErr))
	}
	if err := verifier.profile.validateKey(resolved, metadata.algorithm, metadata.hasAlgorithm); err != nil {
		return VerifiedSignature{}, err
	}
	metadata.result.Algorithm = resolved.Algorithm

	base, err := CreateSignatureBase(message, input)
	if err != nil {
		return VerifiedSignature{}, verificationError(VerificationBase, err)
	}
	if err := Verify(ctx, resolved.Algorithm, resolved.Key, []byte(base), signature.Value); err != nil {
		failure := VerificationCryptographic
		if errors.Is(err, ErrIncompatibleKey) || errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
			failure = VerificationKey
		}
		return VerifiedSignature{}, verificationError(failure, err)
	}

	if metadata.nonce != "" {
		if err := verifier.profile.replay.Consume(ctx, ReplayRecord{
			KeyID:     metadata.keyID,
			Nonce:     metadata.nonce,
			ExpiresAt: metadata.replayExpires,
		}); err != nil {
			return VerifiedSignature{}, verificationError(VerificationReplay, safeReplayCause(err))
		}
	}

	return metadata.result, nil
}

type validatedMetadata struct {
	result        VerifiedSignature
	keyID         string
	nonce         string
	algorithm     Algorithm
	hasAlgorithm  bool
	replayExpires time.Time
}

func (profile *VerificationProfile) validateInput(input SignatureInput) (validatedMetadata, error) {
	metadata := validatedMetadata{result: VerifiedSignature{Label: input.Label}}
	if len(input.Components) == 0 && !profile.allowEmptyCoverage {
		return metadata, verificationError(VerificationPolicy, errors.New("empty coverage"))
	}
	covered := make(map[string]struct{}, len(input.Components))
	for _, component := range input.Components {
		identifier, err := componentComparisonKey(component)
		if err != nil {
			return metadata, verificationError(VerificationPolicy, err)
		}
		covered[identifier] = struct{}{}
	}
	for _, component := range profile.requiredComponents {
		identifier, _ := componentComparisonKey(component)
		if _, exists := covered[identifier]; !exists {
			return metadata, verificationError(VerificationPolicy, errors.New("insufficient coverage"))
		}
	}

	now := profile.now()
	created, hasCreated := integerParameter(input, "created")
	if !presenceAllowed(profile.created, hasCreated) {
		return metadata, verificationError(VerificationPolicy, errors.New("created policy"))
	}
	if hasCreated {
		metadata.result.Created = time.Unix(created, 0)
		if metadata.result.Created.After(now.Add(profile.clockSkew)) ||
			now.Sub(metadata.result.Created) > profile.maxAge+profile.clockSkew {
			return metadata, verificationError(VerificationTime, errors.New("creation time"))
		}
		metadata.replayExpires = metadata.result.Created.Add(profile.maxAge + profile.clockSkew)
	}

	expires, hasExpires := integerParameter(input, "expires")
	if !presenceAllowed(profile.expires, hasExpires) {
		return metadata, verificationError(VerificationPolicy, errors.New("expires policy"))
	}
	if hasExpires {
		metadata.result.Expires = time.Unix(expires, 0)
		if !now.Before(metadata.result.Expires.Add(profile.clockSkew)) ||
			hasCreated && !metadata.result.Expires.After(metadata.result.Created) {
			return metadata, verificationError(VerificationTime, errors.New("expiration time"))
		}
		replayExpiration := metadata.result.Expires.Add(profile.clockSkew)
		if metadata.replayExpires.IsZero() || replayExpiration.Before(metadata.replayExpires) {
			metadata.replayExpires = replayExpiration
		}
	}

	algorithm, hasAlgorithm := stringParameter(input, "alg")
	if !presenceAllowed(profile.algorithmParameter, hasAlgorithm) {
		return metadata, verificationError(VerificationPolicy, errors.New("algorithm parameter policy"))
	}
	metadata.algorithm, metadata.hasAlgorithm = Algorithm(algorithm), hasAlgorithm
	if hasAlgorithm {
		if _, allowed := profile.algorithms[metadata.algorithm]; !allowed {
			return metadata, verificationError(VerificationAlgorithm, errors.New("algorithm not allowed"))
		}
	}

	metadata.keyID, _ = stringParameter(input, "keyid")
	if metadata.keyID == "" {
		return metadata, verificationError(VerificationPolicy, errors.New("key identifier required"))
	}
	metadata.result.KeyID = metadata.keyID

	nonce, hasNonce := stringParameter(input, "nonce")
	metadata.nonce = nonce
	if !presenceAllowed(profile.nonce, hasNonce) || hasNonce && metadata.nonce == "" {
		return metadata, verificationError(VerificationPolicy, errors.New("nonce policy"))
	}
	if hasNonce && metadata.replayExpires.IsZero() {
		return metadata, verificationError(VerificationPolicy, errors.New("nonce lacks bounded lifetime"))
	}

	tag, hasTag := stringParameter(input, "tag")
	if !presenceAllowed(profile.tag, hasTag) {
		return metadata, verificationError(VerificationPolicy, errors.New("tag policy"))
	}
	if hasTag {
		if _, allowed := profile.allowedTags[tag]; !allowed {
			return metadata, verificationError(VerificationPolicy, errors.New("tag not allowed"))
		}
	}

	return metadata, nil
}

func (profile *VerificationProfile) validateKey(key ResolvedKey, parameterAlgorithm Algorithm, hasParameter bool) error {
	now := profile.now()
	if key.Revoked || key.Key == nil || key.NotBefore.IsZero() || key.NotAfter.IsZero() || key.FreshUntil.IsZero() ||
		!key.NotAfter.After(key.NotBefore) || now.Before(key.NotBefore.Add(-profile.clockSkew)) ||
		!now.Before(key.NotAfter.Add(profile.clockSkew)) || !now.Before(key.FreshUntil) {
		return verificationError(VerificationKey, errors.New("unusable key"))
	}
	if _, allowed := profile.algorithms[key.Algorithm]; !allowed {
		return verificationError(VerificationAlgorithm, errors.New("resolved algorithm not allowed"))
	}
	if hasParameter && parameterAlgorithm != key.Algorithm {
		return verificationError(VerificationAlgorithm, errors.New("algorithm mismatch"))
	}

	return nil
}

func selectSignature(label string, inputs SignatureInputs, signatures Signatures) (SignatureInput, SignatureValue, bool) {
	inputIndex := slices.IndexFunc(inputs.entries, func(candidate SignatureInput) bool { return candidate.Label == label })
	if inputIndex == -1 {
		return SignatureInput{}, SignatureValue{}, false
	}
	signatureIndex := slices.IndexFunc(signatures.entries, func(candidate SignatureValue) bool { return candidate.Label == label })
	if signatureIndex == -1 {
		return SignatureInput{}, SignatureValue{}, false
	}
	input := cloneSignatureInput(inputs.entries[inputIndex])
	candidate := signatures.entries[signatureIndex]
	signature := SignatureValue{Label: candidate.Label, Value: append([]byte(nil), candidate.Value...), Parameters: cloneParameters(candidate.Parameters)}
	return input, signature, true
}

func matchingLabelSets(inputs SignatureInputs, signatures Signatures) bool {
	if len(inputs.entries) != len(signatures.entries) {
		return false
	}
	labels := make(map[string]struct{}, len(inputs.entries))
	for _, input := range inputs.entries {
		labels[input.Label] = struct{}{}
	}
	for _, signature := range signatures.entries {
		if _, exists := labels[signature.Label]; !exists {
			return false
		}
	}

	return true
}

func integerParameter(input SignatureInput, name string) (int64, bool) {
	value, exists := input.Parameter(name)
	integer, ok := value.(int64)
	return integer, exists && ok
}

func stringParameter(input SignatureInput, name string) (string, bool) {
	value, exists := input.Parameter(name)
	text, ok := value.(string)
	return text, exists && ok
}

func presenceAllowed(policy ParameterPolicy, present bool) bool {
	return policy == ParameterOptional || policy == ParameterRequired && present || policy == ParameterForbidden && !present
}

func validParameterPolicy(policy ParameterPolicy) bool {
	return policy >= ParameterForbidden && policy <= ParameterRequired
}

func supportedAlgorithm(algorithm Algorithm) bool {
	switch algorithm {
	case RSAPSSSHA512, RSAV15SHA256, HMACSHA256, ECDSAP256SHA256, ECDSAP384SHA384, Ed25519:
		return true
	default:
		return false
	}
}

func verificationError(failure VerificationFailure, cause error) *VerificationError {
	return &VerificationError{Failure: failure, cause: fmt.Errorf("%w", cause)}
}

func safeResolutionCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrKeyNotFound):
		return ErrKeyNotFound
	default:
		return ErrKeyResolutionFailure
	}
}

func safeReplayCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrReplayDetected):
		return ErrReplayDetected
	case errors.Is(err, ErrReplayCapacity):
		return ErrReplayCapacity
	case errors.Is(err, ErrInvalidReplayRecord):
		return ErrInvalidReplayRecord
	default:
		return ErrReplayBackendFailure
	}
}
