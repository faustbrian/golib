package password

import (
	"encoding/base64"
	"errors"
	"io"
	"testing"
)

type nilReaderPointer struct{}

func (*nilReaderPointer) Read([]byte) (int, error) { return 0, io.EOF }

type nilReaderSlice []byte

func (nilReaderSlice) Read([]byte) (int, error) { return 0, io.EOF }

type nilReaderMap map[string]string

func (nilReaderMap) Read([]byte) (int, error) { return 0, io.EOF }

type nilReaderChannel chan struct{}

func (nilReaderChannel) Read([]byte) (int, error) { return 0, io.EOF }

type nilReaderFunction func()

func (nilReaderFunction) Read([]byte) (int, error) { return 0, io.EOF }

func TestNilInterfaceRecognizesEverySupportedNilableKind(t *testing.T) {
	for _, reader := range []io.Reader{
		(*nilReaderPointer)(nil),
		nilReaderSlice(nil),
		nilReaderMap(nil),
		nilReaderChannel(nil),
		nilReaderFunction(nil),
	} {
		if !isNilInterface(reader) {
			t.Fatalf("typed nil %T was accepted", reader)
		}
	}
	if isNilInterface(struct{}{}) {
		t.Fatal("non-nil value was rejected")
	}
}

func TestArgonAndBcryptDecoderFailuresRemainIndependent(t *testing.T) {
	limits := boundaryLimits()
	argon := func(salt, output string) string {
		return "$argon2id$v=19$m=8,t=1,p=1$" + salt + "$" + output
	}
	validSalt := base64.RawStdEncoding.EncodeToString(make([]byte, 8))
	validOutput := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	for _, encoded := range []string{
		argon(validSalt[:len(validSalt)-1]+"B", validOutput),
		argon(validSalt, validOutput[:len(validOutput)-1]+"B"),
	} {
		if _, err := ParseEncodedHash(encoded, limits); !errors.Is(err, ErrMalformedHash) {
			t.Fatalf("non-canonical Argon2 encoding error = %v", err)
		}
	}

	validBcrypt := "$2b$04$" + "......................" + "..............................."
	aliases := []string{
		validBcrypt[:28] + "v" + validBcrypt[29:],
		validBcrypt[:59] + "n",
	}
	for _, encoded := range aliases {
		if _, err := ParseEncodedHash(encoded, limits); !errors.Is(err, ErrMalformedHash) {
			t.Fatalf("non-canonical bcrypt encoding error = %v", err)
		}
	}
}

func TestNeedsRehashRejectsCrossAlgorithmAndVersionDowngrades(t *testing.T) {
	argonPolicy, err := NewPolicy(boundaryArgonConfig())
	if err != nil {
		t.Fatal(err)
	}
	argonService, err := New(argonPolicy)
	if err != nil {
		t.Fatal(err)
	}
	bcryptPolicy, err := NewPolicy(PolicyConfig{Algorithm: Bcrypt, BcryptCost: 4, Limits: boundaryLimits()})
	if err != nil {
		t.Fatal(err)
	}
	bcryptService, err := New(bcryptPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !argonService.NeedsRehash(EncodedHash{algorithm: Bcrypt, bcryptCost: 4}) {
		t.Fatal("Argon2 service did not upgrade bcrypt")
	}
	if bcryptService.NeedsRehash(EncodedHash{algorithm: Argon2id}) {
		t.Fatal("bcrypt service attempted an Argon2 downgrade")
	}
	if argonService.NeedsRehash(EncodedHash{}) {
		t.Fatal("zero hash was treated as a valid cross-algorithm upgrade")
	}

	want := argonPolicy.config.Argon2id
	versionMismatch := want
	versionMismatch.Version++
	versionMismatch.Time--
	if argonService.NeedsRehash(EncodedHash{algorithm: Argon2id, argon2id: versionMismatch}) {
		t.Fatal("version-incompatible Argon2 hash was partially downgraded")
	}
}

func TestArgonParallelismMemoryBoundary(t *testing.T) {
	limits := boundaryLimits()
	encode := func(memory, parallelism int) string {
		return "$argon2id$v=19$m=" + itoa(memory) + ",t=1,p=" + itoa(parallelism) + "$" +
			base64.RawStdEncoding.EncodeToString(make([]byte, 8)) + "$" +
			base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	}
	if _, err := ParseEncodedHash(encode(16, 2), limits); err != nil {
		t.Fatalf("exact memory-per-lane boundary error = %v", err)
	}
	if _, err := ParseEncodedHash(encode(15, 2), limits); !errors.Is(err, ErrResourceRejected) {
		t.Fatalf("under memory-per-lane boundary error = %v", err)
	}
}
