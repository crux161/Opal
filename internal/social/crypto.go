package social

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

var curve = ecdh.X25519()

type Identity struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

type TrustedPeer struct {
	PeerID    string    `json:"peer_id"`
	PublicKey string    `json:"public_key"`
	TrustedAt time.Time `json:"trusted_at"`
}

type TrustState struct {
	Identity Identity               `json:"identity"`
	Trusted  map[string]TrustedPeer `json:"trusted"`
}

func EnsureIdentity(state TrustState) (TrustState, error) {
	if state.Trusted == nil {
		state.Trusted = make(map[string]TrustedPeer)
	}
	if state.Identity.PublicKey != "" && state.Identity.PrivateKey != "" {
		return state, nil
	}

	identity, err := GenerateIdentity()
	if err != nil {
		return TrustState{}, err
	}
	state.Identity = identity
	return state, nil
}

func GenerateIdentity() (Identity, error) {
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		PublicKey:  encode(privateKey.PublicKey().Bytes()),
		PrivateKey: encode(privateKey.Bytes()),
	}, nil
}

func Encrypt(identity Identity, peerPublicKey, plaintext string) (string, error) {
	privateBytes, err := decode(identity.PrivateKey)
	if err != nil {
		return "", err
	}
	privateKey, err := curve.NewPrivateKey(privateBytes)
	if err != nil {
		return "", err
	}
	publicBytes, err := decode(peerPublicKey)
	if err != nil {
		return "", err
	}
	publicKey, err := curve.NewPublicKey(publicBytes)
	if err != nil {
		return "", err
	}

	sharedSecret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return "", err
	}

	key := sha256.Sum256(sharedSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	sealed := aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return encode(payload), nil
}

func Decrypt(identity Identity, peerPublicKey, ciphertext string) (string, error) {
	privateBytes, err := decode(identity.PrivateKey)
	if err != nil {
		return "", err
	}
	privateKey, err := curve.NewPrivateKey(privateBytes)
	if err != nil {
		return "", err
	}
	publicBytes, err := decode(peerPublicKey)
	if err != nil {
		return "", err
	}
	publicKey, err := curve.NewPublicKey(publicBytes)
	if err != nil {
		return "", err
	}

	sharedSecret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return "", err
	}

	key := sha256.Sum256(sharedSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	payload, err := decode(ciphertext)
	if err != nil {
		return "", err
	}
	if len(payload) < aead.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce := payload[:aead.NonceSize()]
	body := payload[aead.NonceSize():]
	opened, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}

	return string(opened), nil
}

func RandomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func encode(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decode(value string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
