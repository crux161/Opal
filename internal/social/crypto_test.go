package social

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	alice, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("generate alice identity: %v", err)
	}
	bob, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("generate bob identity: %v", err)
	}

	ciphertext, err := Encrypt(alice, bob.PublicKey, "hello friend")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	plaintext, err := Decrypt(bob, alice.PublicKey, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if plaintext != "hello friend" {
		t.Fatalf("unexpected plaintext: %q", plaintext)
	}
}

func TestEnsureIdentityInitializesTrustState(t *testing.T) {
	t.Parallel()

	state, err := EnsureIdentity(TrustState{})
	if err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	if state.Identity.PublicKey == "" || state.Identity.PrivateKey == "" {
		t.Fatalf("identity not generated: %#v", state.Identity)
	}
	if state.Trusted == nil {
		t.Fatalf("trusted map not initialized")
	}
}
