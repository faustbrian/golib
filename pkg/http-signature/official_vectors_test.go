package httpsignature

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"testing"
)

func TestRFC9421AppendixBAlgorithmVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		algorithm Algorithm
		key       any
		base      string
		signature string
	}{
		{
			name:      "rsa-pss-sha512",
			algorithm: RSAPSSSHA512,
			key:       rfc9421RSAPSSPublicKey(t),
			base:      `"@signature-params": ();created=1618884473;keyid="test-key-rsa-pss";nonce="b3k2pp5k7z-50gnwp.yemd"`,
			signature: "d2pmTvmbncD3xQm8E9ZV2828BjQWGgiwAaw5bAkgibUopemLJcWDy/lkbbHAve4cRAtx31Iq786U7it++wgGxbtRxf8Udx7zFZsckzXaJMkA7ChG52eSkFxykJeNqsrWH5S+oxNFlD4dzVuwe8DhTSja8xxbR/Z2cOGdCbzR72rgFWhzx2VjBqJzsPLMIQKhO4DGezXehhWwE56YCE+O6c0mKZsfxVrogUvA4HELjVKWmAvtl6UnCh8jYzuVG5WSb/QEVPnP5TmcAnLH1g+s++v6d4s8m0gCw1fV5/SITLq9mhho8K3+7EPYTU8IU1bLhdxO5Nyt8C8ssinQ98Xw9Q==",
		},
		{
			name:      "rsa-v1_5-sha256",
			algorithm: RSAV15SHA256,
			key:       rfc9421RSAV15PublicKey(t),
			base: `"@method": POST
"@authority": origin.host.internal.example
"@path": /foo
"content-digest": sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:
"content-type": application/json
"content-length": 18
"forwarded": for=192.0.2.123;host=example.com;proto=https
"@signature-params": ("@method" "@authority" "@path" "content-digest" "content-type" "content-length" "forwarded");created=1618884480;keyid="test-key-rsa";alg="rsa-v1_5-sha256";expires=1618884540`,
			signature: "S6ZzPXSdAMOPjN/6KXfXWNO/f7V6cHm7BXYUh3YD/fRad4BCaRZxP+JH+8XY1I6+8Cy+CM5g92iHgxtRPz+MjniOaYmdkDcnL9cCpXJleXsOckpURl49GwiyUpZ10KHgOEe11sx3G2gxI8S0jnxQB+Pu68U9vVcasqOWAEObtNKKZd8tSFu7LB5YAv0RAGhB8tmpv7sFnIm9y+7X5kXQfi8NMaZaA8i2ZHwpBdg7a6CMfwnnrtflzvZdXAsD3LH2TwevU+/PBPv0B6NMNk93wUs/vfJvye+YuI87HU38lZHowtznbLVdp770I6VHR6WfgS9ddzirrswsE1w5o0LV/g==",
		},
		{
			name:      "ecdsa-p256-sha256",
			algorithm: ECDSAP256SHA256,
			key:       rfc9421P256PublicKey(t),
			base: `"@status": 200
"content-type": application/json
"content-digest": sha-512=:mEWXIS7MaLRuGgxOBdODa3xqM1XdEvxoYhvlCFJ41QJgJc4GTsPp29l5oGX69wWdXymyU0rjJuahq4l5aGgfLQ==:
"content-length": 23
"@signature-params": ("@status" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-ecc-p256"`,
			signature: "wNmSUAhwb5LxtOtOpNa6W5xj067m5hFrj0XQ4fvpaCLx0NKocgPquLgyahnzDnDAUy5eCdlYUEkLIj+32oiasw==",
		},
		{
			name:      "hmac-sha256",
			algorithm: HMACSHA256,
			key:       rfc9421HMACKey(t),
			base: `"date": Tue, 20 Apr 2021 02:07:55 GMT
"@authority": example.com
"content-type": application/json
"@signature-params": ("date" "@authority" "content-type");created=1618884473;keyid="test-shared-secret"`,
			signature: "pxcQw6G3AjtMBQjwo8XzkZf/bws5LelbaMk5rGIGtE8=",
		},
		{
			name:      "ed25519",
			algorithm: Ed25519,
			key:       ed25519.PublicKey(decodeRawURL(t, "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs")),
			base: `"date": Tue, 20 Apr 2021 02:07:55 GMT
"@method": POST
"@path": /foo
"@authority": example.com
"content-type": application/json
"content-length": 18
"@signature-params": ("date" "@method" "@path" "@authority" "content-type" "content-length");created=1618884473;keyid="test-key-ed25519"`,
			signature: "wqcAqbmYJ2ji2glfAMaRy4gruYYnx2nEFN2HN6jrnDnQCK1u02Gb04v9EDgwUPiu4A0w6vuQv5lIp5WPpBKRCw==",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			signature, err := base64.StdEncoding.DecodeString(test.signature)
			if err != nil {
				t.Fatalf("DecodeString(signature) error = %v", err)
			}
			if err := Verify(context.Background(), test.algorithm, test.key, []byte(test.base), signature); err != nil {
				t.Fatalf("Verify(RFC 9421 Appendix B) error = %v", err)
			}
		})
	}
}

func TestNISTP384SHA384VerificationVector(t *testing.T) {
	t.Parallel()

	// NIST CAVP FIPS 186-3 SigVer.rsp, [P-384,SHA-384], Result = P.
	message := decodeHex(t, "9dd789ea25c04745d57a381f22de01fb0abd3c72dbdefd44e43213c189583eef85ba662044da3de2dd8670e6325154480155bbeebb702c75781ac32e13941860cb576fe37a05b757da5b5b418f6dd7c30b042e40f4395a342ae4dce05634c33625e2bc524345481f7e253d9551266823771b251705b4a85166022a37ac28f1bd")
	publicKey := &ecdsa.PublicKey{
		Curve: elliptic.P384(),
		X:     new(big.Int).SetBytes(decodeHex(t, "cb908b1fd516a57b8ee1e14383579b33cb154fece20c5035e2b3765195d1951d75bd78fb23e00fef37d7d064fd9af144")),
		Y:     new(big.Int).SetBytes(decodeHex(t, "cd99c46b5857401ddcff2cf7cf822121faf1cbad9a011bed8c551f6f59b2c360f79bfbe32adbcaa09583bdfdf7c374bb")),
	}
	signature := append(
		decodeHex(t, "33f64fb65cd6a8918523f23aea0bbcf56bba1daca7aff817c8791dc92428d605ac629de2e847d43cee55ba9e4a0e83ba"),
		decodeHex(t, "4428bb478a43ac73ecd6de51ddf7c28ff3c2441625a081714337dd44fea8011bae71959a10947b6ea33f77e128d3c6ae")...,
	)
	if err := Verify(context.Background(), ECDSAP384SHA384, publicKey, message, signature); err != nil {
		t.Fatalf("Verify(NIST P-384/SHA-384) error = %v", err)
	}
}

func rfc9421RSAPSSPublicKey(t *testing.T) *rsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(`-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAr4tmm3r20Wd/PbqvP1s2
+QEtvpuRaV8Yq40gjUR8y2Rjxa6dpG2GXHbPfvMs8ct+Lh1GH45x28Rw3Ry53mm+
oAXjyQ86OnDkZ5N8lYbggD4O3w6M6pAvLkhk95AndTrifbIFPNU8PPMO7OyrFAHq
gDsznjPFmTOtCEcN2Z1FpWgchwuYLPL+Wokqltd11nqqzi+bJ9cvSKADYdUAAN5W
Utzdpiy6LbTgSxP7ociU4Tn0g5I6aDZJ7A8Lzo0KSyZYoA485mqcO0GVAdVw9lq4
aOT9v6d+nb4bnNkQVklLQ3fVAvJm+xdDOp9LCNCN48V2pnDOkFV6+U9nV5oyc6XI
2wIDAQAB
-----END PUBLIC KEY-----`))
	if block == nil {
		t.Fatal("PEM decode failed")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey() error = %v", err)
	}
	publicKey, ok := key.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("public key type = %T", key)
	}
	return publicKey
}

func rfc9421RSAV15PublicKey(t *testing.T) *rsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(`-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEAhAKYdtoeoy8zcAcR874L8cnZxKzAGwd7v36APp7Pv6Q2jdsPBRrw
WEBnez6d0UDKDwGbc6nxfEXAy5mbhgajzrw3MOEt8uA5txSKobBpKDeBLOsdJKFq
MGmXCQvEG7YemcxDTRPxAleIAgYYRjTSd/QBwVW9OwNFhekro3RtlinV0a75jfZg
kne/YiktSvLG34lw2zqXBDTC5NHROUqGTlML4PlNZS5Ri2U4aCNx2rUPRcKIlE0P
uKxI4T+HIaFpv8+rdV6eUgOrB2xeI1dSFFn/nnv5OoZJEIB+VmuKn3DCUcCZSFlQ
PSXSfBDiUGhwOw76WuSSsf1D4b/vLoJ10wIDAQAB
-----END RSA PUBLIC KEY-----`))
	if block == nil {
		t.Fatal("PEM decode failed")
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKCS1PublicKey() error = %v", err)
	}
	return key
}

func rfc9421P256PublicKey(t *testing.T) *ecdsa.PublicKey {
	t.Helper()
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(decodeRawURL(t, "qIVYZVLCrPZHGHjP17CTW0_-D9Lfw0EkjqF7xB4FivA")),
		Y:     new(big.Int).SetBytes(decodeRawURL(t, "Mc4nN9LTDOBhfoUeg8Ye9WedFRhnZXZJA12Qp0zZ6F0")),
	}
}

func rfc9421HMACKey(t *testing.T) HMACKey {
	t.Helper()
	material, err := base64.StdEncoding.DecodeString("uzvJfB4u3N0Jy4T7NZ75MDVcr8zSTInedJtkgcu46YW4XByzNJjxBdtjUkdJPBtbmHhIDi6pcl8jsasjlTMtDQ==")
	if err != nil {
		t.Fatalf("DecodeString(shared secret) error = %v", err)
	}
	key, err := NewHMACKey(material)
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}
	return key
}

func decodeRawURL(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	return decoded
}

func decodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	return decoded
}
